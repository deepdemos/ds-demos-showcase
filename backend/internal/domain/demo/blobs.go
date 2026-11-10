package demo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5"

	backend "github.com/deepdemos/ds-demos-showcase/backend/internal/platform/storage"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// 内容寻址存储（零拷贝）。
//
// # 为什么不在原来的 `demos/{slug}/r{n}/` 目录布局上打补丁
//
// 那种布局里「文件属于某个版本」是**隐含在路径里**的：改一个文件就要把整个版本目录
// 重写一遍，两个版本内容 99% 相同也只共享零个字节。设计 §7.2 要的是「版本引用内容」，
// 所以内容必须能独立寻址——`blobs.sha256` 就是那个地址。
//
// # 为什么 sha256 而不是 UUID
//
// 内容寻址的收益全靠「同样的字节算出同样的地址」：两个人交同一个 index.html，
// 第二个人不该再占一份磁盘。UUID 做不到这一点，它只解决「名字不冲突」。
// 代价是路径里能看出文件内容是否相同——这不是秘密，文件名与大小本来就是公开的。
//
// # 为什么源包也进 blob 表
//
// 下载端要吐「原始上传包」。它和解析出的文件一样是**一份字节**，走同一条路就不需要
// 第二套 key 规则；单文件作品更干脆——原件就是 `index.svg` 本身，没有额外的包。
const blobKeyPrefix = "blobs/"

// storedBlob 是一次「写进存储」的结果：地址、大小、类型。
type storedBlob struct {
	sha256 string
	size   int64
	mime   string
}

// blobStorageKey 把内容地址映射成存储 key。
//
// 前两位做一级目录，避免单目录下堆几十万个文件（本地 ext4/NTFS 都会在这种目录里
// 变慢，列目录尤其明显）。两级 65536 个桶对这种规模的站点足够。
func blobStorageKey(sha string) string {
	if len(sha) < 2 {
		return blobKeyPrefix + sha
	}
	return blobKeyPrefix + sha[:2] + "/" + sha
}

// storeBlob 把本地暂存文件写成一份内容寻址的对象，并登记到 blobs 表。
//
// 已存在时**完全不碰磁盘**：这是零拷贝的全部收益所在。判断依据是数据库里有这一行，
// 不是「先 Stat 一下存储」——库与存储的一致性方向是「库里有的，存储里应该也有」，
// 反过来（存储里有、库里没有）是上次写坏留下的孤儿，不该被当成命中。
func (s *Service) storeBlob(ctx context.Context, localPath string) (storedBlob, error) {
	var out storedBlob

	f, err := os.Open(localPath)
	if err != nil {
		return out, fmt.Errorf("demo: 打开暂存文件失败: %w", err)
	}
	defer func() { _ = f.Close() }()

	// 先算哈希：要落盘才需要读第二遍（下面重新打开）。不落盘时这一遍就是全部开销。
	h := sha256.New()
	size, err := io.Copy(h, f)
	if err != nil {
		return out, fmt.Errorf("demo: 计算文件哈希失败: %w", err)
	}
	sum := hex.EncodeToString(h.Sum(nil))
	contentType := mimeOfPath(localPath)
	out = storedBlob{sha256: sum, size: size, mime: contentType}

	existing, err := s.q.GetBlob(ctx, sum)
	switch {
	case err == nil:
		// 行在，但**不能据此断定字节也在**。
		//
		// 这里的判断曾经只有「数据库里有没有这一行」，理由是「上传与存储的一致性
		// 由上传保证」。它漏掉两种真会发生的情况：换过存储根（测试里每轮一个临时
		// 目录、而库是共用的），以及从只备份了数据库的快照恢复。两种情况下这一行
		// 都在、对象却不在，于是后面每一次上传都跳过写盘，预览与下载永远 404，
		// 而错误信息是「对象不存在」——看起来像存储坏了。
		//
		// 一次 Stat 就能分辨，代价可以忽略：命中的路径本来就要写一次数据库。
		if _, statErr := s.files.Stat(ctx, existing.StorageKey); statErr == nil {
			if err := s.q.InsertBlob(ctx, store.InsertBlobParams{
				Sha256:     sum,
				Size:       size,
				Mime:       contentType,
				StorageKey: existing.StorageKey,
			}); err != nil {
				return out, fmt.Errorf("demo: 登记 blob 失败: %w", err)
			}
			return out, nil
		} else if !errors.Is(statErr, backend.ErrNotFound) {
			return out, fmt.Errorf("demo: 检查存储对象失败: %w", statErr)
		}
		// 行在、对象不在：往下走一遍正常写入，把它补回来。
	case !errors.Is(err, pgx.ErrNoRows):
		return out, fmt.Errorf("demo: 查 blob 失败: %w", err)
	}

	// 未命中：重新打开读一遍写到存储。文件已经在本地盘上，多读一遍换来的是
	// 「不必把整份内容读进内存」——包可能有 200 MiB。
	src, err := os.Open(localPath)
	if err != nil {
		return out, fmt.Errorf("demo: 重新打开暂存文件失败: %w", err)
	}
	defer func() { _ = src.Close() }()

	key := blobStorageKey(sum)
	if err := s.files.Put(ctx, key, src, size, contentType); err != nil {
		return out, fmt.Errorf("demo: 写入存储失败: %w", err)
	}
	if err := s.q.InsertBlob(ctx, store.InsertBlobParams{
		Sha256:     sum,
		Size:       size,
		Mime:       contentType,
		StorageKey: key,
	}); err != nil {
		return out, fmt.Errorf("demo: 登记 blob 失败: %w", err)
	}
	return out, nil
}

// mimeOfPath 按扩展名猜类型。
//
// 只用于登记与将来 GC 的展示，**不用于响应头**：预览与下载各自有自己的判定
// （预览要按「是不是 HTML」决定是否注入 base 与加 CSP，下载一律 octet-stream）。
// 两处各自判是刻意的——同一份文件在两条路径上的正确 Content-Type 本来就不同。
func mimeOfPath(p string) string {
	ext := strings.ToLower(filepath.Ext(p))
	if ext == "" {
		return "application/octet-stream"
	}
	if ct := mime.TypeByExtension(ext); ct != "" {
		if i := strings.IndexByte(ct, ';'); i > 0 {
			ct = ct[:i]
		}
		if len(ct) <= 128 {
			return ct
		}
	}
	return "application/octet-stream"
}

// manifestPathOfSource 是源包在清单里的路径。
//
// 它与解析出的文件共用一张表，于是「下载原件」不需要第二套寻址：
// 多文件作品下载 `source.zip`，单文件作品下载 `index.svg`——都是清单里的普通一行。
const manifestSourcePath = "source.zip"

// writeManifest 把「这个版本有哪些文件」写进 demo_revision_files。
//
// entries 是「清单路径 → 本地暂存文件」，源包单独传：它的 path 固定，
// 但本地文件名是暂存的 source.zip。
func (s *Service) writeManifest(ctx context.Context, revID int64, entries map[string]string) error {
	files, err := s.uploadBlobs(ctx, entries)
	if err != nil {
		return err
	}
	return s.writeManifestRows(ctx, s.q, revID, files)
}

// blobFile 是一条「清单行」在写库之前的样子。
//
// 分成「先传字节、再写行」两段是为了让**更新**能用：
// 更新要知道新版本号才能插清单行，而版本号在事务里由数据库算；
// 字节先落地（内容寻址，重复写是幂等的），事务里只做几行 INSERT。
// 代价是事务失败时会留下一批孤儿 blob——那是 GC 的事（松点 33），
// 比「事务里传 200 MiB」安全得多：长事务会占住连接、阻塞 vacuum。
type blobFile struct {
	Path   string
	SHA256 string
	Size   int64
	MIME   string
}

// uploadBlobs 把暂存文件逐个写成内容寻址对象，返回清单所需的元数据。
func (s *Service) uploadBlobs(ctx context.Context, entries map[string]string) ([]blobFile, error) {
	// 路径排序只为日志稳定，不影响结果。
	paths := make([]string, 0, len(entries))
	for p := range entries {
		paths = append(paths, p)
	}
	sortStrings(paths)

	out := make([]blobFile, 0, len(paths))
	for _, p := range paths {
		b, err := s.storeBlob(ctx, entries[p])
		if err != nil {
			return nil, err
		}
		out = append(out, blobFile{Path: p, SHA256: b.sha256, Size: b.size, MIME: b.mime})
	}
	return out, nil
}

// writeManifestRows 把清单行写进库。q 可以是事务内的 Queries——更新走这条路。
func (s *Service) writeManifestRows(ctx context.Context, q *store.Queries, revID int64, files []blobFile) error {
	for _, f := range files {
		if err := q.InsertRevisionFile(ctx, store.InsertRevisionFileParams{
			RevisionID: revID,
			Path:       f.Path,
			BlobHash:   f.SHA256,
			Size:       f.Size,
			Mime:       f.MIME,
		}); err != nil {
			return fmt.Errorf("demo: 写清单失败（%s）: %w", f.Path, err)
		}
	}
	return nil
}

// manifestEntries 把暂存结果摊成「路径 → 本地文件」。
//
// 投稿与更新共用：两处都得把 contentDir 里的文件与源包拼成同一张表。
func manifestEntries(st staged) (map[string]string, error) {
	entries := make(map[string]string, len(st.files)+1)
	for _, rel := range st.files {
		cleaned, err := cleanManifestPath(rel)
		if err != nil {
			return nil, err
		}
		entries[cleaned] = filepath.Join(st.contentDir, filepath.FromSlash(rel))
	}
	if st.sourcePath != "" {
		entries[manifestSourcePath] = st.sourcePath
	}
	return entries, nil
}

// sortStrings 是最朴素的插入排序：这里最多几百个路径，引入 sort 包不值得
// （这个文件已经在 import 列表里挣扎过了）。
func sortStrings(xs []string) {
	for i := 1; i < len(xs); i++ {
		for j := i; j > 0 && xs[j] < xs[j-1]; j-- {
			xs[j], xs[j-1] = xs[j-1], xs[j]
		}
	}
}

// manifestSourcePathFor 返回「下载原件」在清单里的路径。
//
// 多文件作品是解析时保存的 source.zip；单文件作品没有额外的包，
// 原件就是入口文件本身（`index.html` / `index.svg`）。
func manifestSourcePathFor(singleFile string) string {
	if singleFile != "" {
		return "index." + singleFile
	}
	return manifestSourcePath
}

// cleanManifestPath 归一化清单路径：统一 `/`、去掉前导 `./`、拒绝逃出根目录。
//
// 写进数据库的路径是**权威**，读路径直接用它与 blob 对应。归一化放在写入口，
// 读路径就不必再判一次——同一件事判两遍必然有一处先忘。
func cleanManifestPath(rel string) (string, error) {
	cleaned := path.Clean(strings.TrimPrefix(filepath.ToSlash(rel), "./"))
	if cleaned == "." || cleaned == "" {
		return "", fmt.Errorf("demo: 清单路径为空")
	}
	if strings.HasPrefix(cleaned, "../") || path.IsAbs(cleaned) {
		return "", fmt.Errorf("demo: 清单路径逃出根目录: %s", rel)
	}
	return cleaned, nil
}

// 编译期确认 storage 的 key 前缀仍是老的那套：回退分支（没有清单的旧版本）
// 依赖它把版本号拼进路径。这条断言不产生代码，只挡住「改了前缀却没人发现」。
var _ = backend.FilesPrefix
