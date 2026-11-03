package storage

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// 解压相关的哨兵错误。调用方（Demo 域）据此映射成 4xx，不要把底层文字直接给用户。
var (
	// ErrUnsafePath 成员名试图逃出目标目录（`..`、绝对路径、软链等）。
	ErrUnsafePath = errors.New("storage: 压缩包内含非法路径")
	// ErrTooManyMembers 成员数超过上限。
	ErrTooManyMembers = errors.New("storage: 压缩包成员数超过上限")
	// ErrTooLarge 解压后体积超过上限。
	ErrTooLarge = errors.New("storage: 解压后体积超过上限")
	// ErrSuspiciousRatio 压缩比异常（疑似压缩炸弹）。
	ErrSuspiciousRatio = errors.New("storage: 压缩比异常")
	// ErrEmptyArchive 压缩包里没有任何文件。
	ErrEmptyArchive = errors.New("storage: 压缩包内没有文件")
)

// Limits 是解压的四道数值闸。
//
// 值来自运行手册 §3「安全下限」，由配置注入而不是写死在这里——但**它们不该被做成运营
// 参数**：调大就等于调高被 zip 炸弹打满磁盘的风险（判据见 `docs/设置域设计.md` §4.1 的 J2）。
type Limits struct {
	// MaxMembers 成员数上限。
	MaxMembers int
	// MaxUncompressed 累计解压字节上限。
	MaxUncompressed int64
	// MaxRatio 累计压缩比上限（解压 ÷ 压缩）。
	MaxRatio float64
	// RatioMinBytes 压缩比检查的启用门槛：只有累计解压量达到这个值才判压缩比。
	//
	// 理由：解压后总共才几百 KB 的包，压缩比再高也不构成磁盘威胁，而极小文件的压缩比
	// 天然可以极大（几十字节的重复内容）。没有这道门槛会把正常的小包误判成炸弹。
	RatioMinBytes int64
}

// DefaultLimits 返回与运行手册一致的默认值。
func DefaultLimits() Limits {
	return Limits{
		MaxMembers:      2000,
		MaxUncompressed: 500 << 20, // 500 MiB
		MaxRatio:        100,
		RatioMinBytes:   4 << 20, // 4 MiB
	}
}

// ExtractStats 是一次成功解压的账。
type ExtractStats struct {
	Files        int
	Uncompressed int64
	Compressed   int64
}

// ExtractZip 把压缩包安全解压到 dst。
//
// 契约：
//   - dst 必须**不存在**（调用方传一个全新的暂存目录）。成功时它存在且内容完整。
//   - 任何失败都会把 dst 整个删掉再返回错误——**绝不留下半成品**，调用方不需要清理。
//   - 不跟随软链、不写隐藏文件、不接受绝对路径与 `..`。
//
// 五道闸的顺序是有意的：先看路径（最便宜，且能挡住最危险的一类），再看数量与体积，
// 最后才是压缩比——压缩比要在累计量够大时才有意义（见 Limits.RatioMinBytes）。
func ExtractZip(ctx context.Context, zr *zip.Reader, dst string, lim Limits) (ExtractStats, error) {
	var stats ExtractStats
	// written 是**实际**落盘字节数，与 stats.Uncompressed（头部声明值）分开记：
	// 前者是防线，后者只用来做压缩比的粗略预判。
	var written int64

	if lim.MaxMembers <= 0 || lim.MaxUncompressed <= 0 {
		return stats, errors.New("storage: 解压上限未配置")
	}
	// 中央目录里的成员数是已知的，先粗筛一遍，省掉大包的前期 IO。
	if len(zr.File) > lim.MaxMembers {
		return stats, fmt.Errorf("%w（%d > %d）", ErrTooManyMembers, len(zr.File), lim.MaxMembers)
	}

	// dst 必须是全新的：已存在就拒，避免把调用方已有目录里的东西混进来（也避免失败时误删）。
	if _, err := os.Stat(dst); err == nil {
		return stats, fmt.Errorf("storage: 解压目标已存在: %s", dst)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return stats, fmt.Errorf("storage: 检查解压目标失败: %w", err)
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return stats, fmt.Errorf("storage: 创建解压目录失败: %w", err)
	}
	// 无论成功失败，先挂上清理；成功时把 cleaned 置 true。
	success := false
	defer func() {
		if !success {
			_ = os.RemoveAll(dst)
		}
	}()

	for _, f := range zr.File {
		if err := ctx.Err(); err != nil {
			return stats, err
		}

		rel, err := safeRelName(f)
		if err != nil {
			return stats, err
		}
		if rel == "" { // 目录条目或隐藏文件，跳过
			continue
		}

		stats.Files++
		if stats.Files > lim.MaxMembers {
			return stats, fmt.Errorf("%w（> %d）", ErrTooManyMembers, lim.MaxMembers)
		}

		// 先按头部声明累加——头部会撒谎，所以下面真正写的时候还要再卡一次实际字节数。
		stats.Uncompressed += int64(f.UncompressedSize64)
		if stats.Uncompressed > lim.MaxUncompressed {
			return stats, fmt.Errorf("%w（> %d 字节）", ErrTooLarge, lim.MaxUncompressed)
		}
		stats.Compressed += int64(f.CompressedSize64)
		// 压缩比每写一个成员就重判一次：累计口径必须边写边看，等全部写完再判就已经落盘了。
		if err := CheckRatio(stats, lim); err != nil {
			return stats, err
		}

		n, err := writeMember(ctx, f, dst, rel, written, lim)
		if err != nil {
			return stats, err
		}
		written += n
	}

	if stats.Files == 0 {
		return stats, ErrEmptyArchive
	}
	success = true
	return stats, nil
}

// safeRelName 把成员名变成可信的相对路径；返回空串表示「目录条目，跳过」。
func safeRelName(f *zip.File) (string, error) {
	// 软链：zip 用 external attributes 携带，解出来会指向目标目录之外。
	// 直接拒而不是跳过——一个包里出现软链本身就说明它不是普通静态资源。
	if f.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("%w: 含符号链接 %q", ErrUnsafePath, f.Name)
	}

	raw := strings.ReplaceAll(f.Name, "\\", "/")
	if strings.HasSuffix(raw, "/") {
		return "", nil // 目录
	}
	if raw == "" {
		return "", nil
	}
	// 绝对路径直接拒，而不是「把开头的 / 去掉当相对路径」——
	// 静态资源包里出现绝对路径说明它本就不是按相对路径打的，静默纠正等于替它猜意图。
	if strings.HasPrefix(raw, "/") {
		return "", fmt.Errorf("%w: 含绝对路径 %q", ErrUnsafePath, f.Name)
	}
	if len(raw) >= 2 && raw[1] == ':' {
		return "", fmt.Errorf("%w: 含盘符 %q", ErrUnsafePath, f.Name)
	}

	parts := make([]string, 0, 4)
	for _, seg := range strings.Split(raw, "/") {
		switch seg {
		case "", ".":
			continue
		case "..":
			// 不做归一化：`a/../b` 悄悄变成 `b` 会让人以为写对了。
			return "", fmt.Errorf("%w: 含 `..` 段 %q", ErrUnsafePath, f.Name)
		}
		// 隐藏文件：`.env`、`.git/config` 这类东西不该随作品发出去，也常被托管层特殊对待。
		if strings.HasPrefix(seg, ".") {
			return "", nil // 当作跳过
		}
		parts = append(parts, seg)
	}
	if len(parts) == 0 {
		return "", nil
	}
	rel := strings.Join(parts, "/")
	// 最后一道：确认它确实是本地相对路径（挡盘符、挡某个平台特有的绝对路径写法）。
	if !filepath.IsLocal(filepath.FromSlash(rel)) {
		return "", fmt.Errorf("%w: 非法相对路径 %q", ErrUnsafePath, f.Name)
	}
	return rel, nil
}

// writeMember 把单个成员写进 dst/rel，并对**实际写入字节数**再卡一次上限。
// 返回本次实际写入的字节数。
func writeMember(ctx context.Context, f *zip.File, dst, rel string, written int64, lim Limits) (int64, error) {
	remaining := lim.MaxUncompressed - written
	if remaining <= 0 {
		return 0, fmt.Errorf("%w（> %d 字节）", ErrTooLarge, lim.MaxUncompressed)
	}

	rc, err := f.Open()
	if err != nil {
		return 0, fmt.Errorf("storage: 读取压缩成员失败 %q: %w", f.Name, err)
	}
	defer func() { _ = rc.Close() }()

	full := filepath.Join(dst, filepath.FromSlash(rel))
	// 再核一次：safeRelName 已经挡过 `..`，这里防的是拼接之后才出现的逃逸。
	if within, err := isWithin(dst, full); err != nil || !within {
		return 0, fmt.Errorf("%w: %q", ErrUnsafePath, f.Name)
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return 0, fmt.Errorf("storage: 创建子目录失败: %w", err)
	}

	out, err := os.Create(full)
	if err != nil {
		return 0, fmt.Errorf("storage: 创建文件失败 %q: %w", rel, err)
	}
	// 多读一个字节：读到即说明实际解压量超过上限，头部声明的 size 在撒谎。
	n, copyErr := io.Copy(out, io.LimitReader(rc, remaining+1))
	closeErr := out.Close()
	if copyErr != nil {
		return n, fmt.Errorf("storage: 解压写入失败 %q: %w", rel, copyErr)
	}
	if closeErr != nil {
		return n, fmt.Errorf("storage: 关闭文件失败 %q: %w", rel, closeErr)
	}
	if n > remaining {
		return n, fmt.Errorf("%w（成员 %q 实际解压超过剩余额度）", ErrTooLarge, f.Name)
	}
	if err := ctx.Err(); err != nil {
		return n, err
	}
	return n, nil
}

// isWithin 报告 target 是否在 root 之内（root 自身也算）。
func isWithin(root, target string) (bool, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return false, err
	}
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return false, err
	}
	return absTarget == absRoot || strings.HasPrefix(absTarget, absRoot+string(os.PathSeparator)), nil
}

// CheckRatio 单独暴露给调用方做「解压前预判」用（例如先看中央目录再决定要不要真解）。
func CheckRatio(stats ExtractStats, lim Limits) error {
	if stats.Compressed <= 0 || stats.Uncompressed < lim.RatioMinBytes {
		return nil
	}
	if float64(stats.Uncompressed)/float64(stats.Compressed) > lim.MaxRatio {
		return fmt.Errorf("%w（%.0f:1 > %.0f:1）",
			ErrSuspiciousRatio,
			float64(stats.Uncompressed)/float64(stats.Compressed), lim.MaxRatio)
	}
	return nil
}
