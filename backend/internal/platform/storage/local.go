package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Local 把对象存成磁盘上的文件。本期唯一的实现，也是真相源。
//
// 为什么本地是真相源而不是 OSS：OSS 推送失败时作品还能用；反过来「OSS 是真相源」，
// 一旦推失败作品就是坏的。读的时候优先本地，本地没有（换了机器、清过盘）再回源。
type Local struct {
	root string // 已 resolve 的绝对路径，不带尾部分隔符
}

// NewLocal 创建（必要时先建）根目录。
func NewLocal(root string) (*Local, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("storage: 本地存储根目录不能为空")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("storage: 解析根目录失败: %w", err)
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, fmt.Errorf("storage: 创建根目录失败: %w", err)
	}
	return &Local{root: abs}, nil
}

// Root 返回解析后的根目录绝对路径。
//
// 供需要真实路径的调用方使用（例如后续要起静态文件服务）。走 Store 接口的代码不应依赖它。
func (l *Local) Root() string { return l.root }

// resolve 把 key 变成根目录下的绝对路径，并确认它没有逃出根目录。
//
// 三段校验，缺一不可：
//  1. key 必须是「干净的相对路径」——任何空段、`.`、`..` 直接拒（不做归一化，
//     归一化会把 `a/../b` 悄悄变成 `b`，让调用方以为自己写对了）
//  2. 不接受绝对路径、盘符、反斜杠（key 的方言只有一种：`/` 分隔）
//  3. 拼出来的绝对路径必须仍在根目录下——防住平台差异导致的意外逃逸
func (l *Local) resolve(key string) (string, error) {
	if key == "" {
		return "", fmt.Errorf("%w: 空 key", ErrUnsafeKey)
	}
	if strings.ContainsAny(key, "\\:\x00") {
		return "", fmt.Errorf("%w: key 含非法字符: %q", ErrUnsafeKey, key)
	}
	for _, seg := range strings.Split(key, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return "", fmt.Errorf("%w: key 含非法路径段: %q", ErrUnsafeKey, key)
		}
	}
	if path.IsAbs(key) || !filepath.IsLocal(key) {
		return "", fmt.Errorf("%w: key 不是本地相对路径: %q", ErrUnsafeKey, key)
	}
	full := filepath.Join(l.root, filepath.FromSlash(key))
	// 再核一次：Join 已经把 .. 折进去了，这里比对前缀是最后一道。
	if full != l.root && !strings.HasPrefix(full, l.root+string(os.PathSeparator)) {
		return "", fmt.Errorf("%w: key 逃出根目录: %q", ErrUnsafeKey, key)
	}
	return full, nil
}

// Put 原子写入：先写同目录临时文件再 rename。读者要么看到旧内容，要么看到完整新内容。
func (l *Local) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	full, err := l.resolve(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return fmt.Errorf("storage: 创建目录失败: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(full), ".tmp-put-*")
	if err != nil {
		return fmt.Errorf("storage: 创建临时文件失败: %w", err)
	}
	tmpName := tmp.Name()
	// 失败路径统一清理；成功路径已经把文件 rename 走了，Remove 会 ENOENT，忽略即可。
	defer func() { _ = os.Remove(tmpName) }()

	// size 只作为期望值：真实写入超过它就报错，防止调用方算错上限导致写满磁盘。
	var written int64
	if size > 0 {
		written, err = io.Copy(tmp, io.LimitReader(r, size+1))
		if err == nil && written > size {
			_ = tmp.Close()
			return fmt.Errorf("storage: 写入超过声明大小 %d 字节", size)
		}
	} else {
		written, err = io.Copy(tmp, r)
	}
	if err != nil {
		_ = tmp.Close()
		return fmt.Errorf("storage: 写入失败: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("storage: 关闭临时文件失败: %w", err)
	}
	_ = written
	if err := os.Rename(tmpName, full); err != nil {
		return fmt.Errorf("storage: 落盘失败: %w", err)
	}
	return nil
}

func (l *Local) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	full, err := l.resolve(key)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(full)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("storage: 打开失败: %w", err)
	}
	return f, nil
}

func (l *Local) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	full, err := l.resolve(key)
	if err != nil {
		return err
	}
	// RemoveAll 对不存在的路径返回 nil，天然幂等。
	return os.RemoveAll(full)
}

func (l *Local) Stat(ctx context.Context, key string) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	full, err := l.resolve(key)
	if err != nil {
		return 0, err
	}
	info, err := os.Stat(full)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return 0, ErrNotFound
		}
		return 0, fmt.Errorf("storage: 取信息失败: %w", err)
	}
	return info.Size(), nil
}

// URL 返回 false：本地文件没有浏览器可直接访问的地址。
//
// 这正是将来要接 OSS 的理由之一——但「能不能直链」不该渗透到调用方，
// 调用方只应把 false 当成「走我们自己的下载端点」。
func (l *Local) URL(string) (string, bool) { return "", false }
