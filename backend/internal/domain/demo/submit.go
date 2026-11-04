package demo

import (
	"archive/zip"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/platform/storage"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// submitTimeout 是一次投稿的总时长上限。
//
// 比普通查询宽得多：一个 200 MiB 的包要解压、要算哈希、要逐个写到存储。
// 它不是「慢查询」保护，而是「客户端连不上时不要永远占着 goroutine」的兜底。
const submitTimeout = SubmitTimeout

// SubmitTimeout 是一次投稿的总时长上限。传出去是为了让传输层与领域层
// 用同一个值——两处各写一个 5 分钟，改一处另一处不会跟着变。
const SubmitTimeout = 5 * time.Minute

// Service 是作品域的服务。
type Service struct {
	pool  *pgxpool.Pool
	q     *store.Queries
	files storage.Store
	// limits 是解压的四道数值闸，由配置注入（默认值与运行手册一致）。
	limits storage.Limits
	// maxPackageBytes 是上传包本身的体积上限。
	maxPackageBytes int64
	// maxPending 是一个作者同时能压多少条待审。0 表示不限。
	maxPending int
	log        *slog.Logger
}

// New 组装服务。files 与 q 都必须非 nil——投稿没有「降级运行」这一说。
func New(pool *pgxpool.Pool, q *store.Queries, files storage.Store, limits storage.Limits, maxPackageBytes int64, maxPending int, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{pool: pool, q: q, files: files, limits: limits, maxPackageBytes: maxPackageBytes, maxPending: maxPending, log: log}
}

// MaxPackageBytes 返回上传包的体积上限（字节）。
func (s *Service) MaxPackageBytes() int64 { return s.maxPackageBytes }

// staged 是一次投稿在本地暂存出来的东西。
type staged struct {
	// dir 是整个暂存目录（原始包也放这里）。
	dir string
	// contentDir 是**待上传文件**所在的根：zip 是 dir/content，单文件是 dir 本身。
	// 上传时必须用它拼路径，不能用 dir——那是本文件曾经踩过的坑。
	contentDir string
	// files 是相对路径列表（`/` 分隔），与 dir 里的真实文件一一对应。
	files []string
	// entryPath 是入口文件的相对路径。
	entryPath string
	// singleFile 非空表示这是单文件作品（`html` / `svg`）。
	singleFile string
	// sourceKey/sourcePath 是原始上传包（zip 才有；单文件不额外存一份）。
	sourceKey  string
	sourcePath string
	totalBytes int64
	manifest   string
}

// Submit 处理一次投稿。
//
// 顺序：校验元数据 → 暂存（解压/校验）→ 落库（一个短事务）→ 上传文件 → 完成。
//
// 为什么不是「先上传再落库」：上传需要 slug，而 slug 要等数据库判唯一。
// 为什么不是「落库与上传同一个事务」：上传 200 MiB 可能几十秒，长事务会占住连接、
// 阻塞 vacuum。折中是**先落库再上传，失败就把这一行删掉**——这一行是同一请求里刚建的、
// 从未对外可见（`draft` + `pending`），删它不构成数据丢失。
func (s *Service) Submit(ctx context.Context, in SubmitInput) (SubmitResult, error) {
	var out SubmitResult

	if err := in.validate(); err != nil {
		return out, err
	}
	if s.maxPackageBytes > 0 && in.PackageSize > s.maxPackageBytes {
		return out, fmt.Errorf("%w（%d > %d 字节）", ErrPackageTooLarge, in.PackageSize, s.maxPackageBytes)
	}
	// 配额判在解压之前：解压是全站最贵的一步（最多 500 MiB），
	// 等解完再拒等于先付出成本再告诉对方不行。
	if s.maxPending > 0 {
		n, err := s.q.CountPendingDemosByAuthor(ctx, int8(in.AuthorID))
		if err != nil {
			return out, fmt.Errorf("demo: 查待审数量失败: %w", err)
		}
		if int(n) >= s.maxPending {
			return out, fmt.Errorf("%w：最多同时有 %d 条待审作品，请等已提交的审核完再投", ErrTooManyPending, s.maxPending)
		}
	}

	ctx, cancel := context.WithTimeout(ctx, submitTimeout)
	defer cancel()

	st, cleanup, err := s.stage(ctx, in)
	if err != nil {
		return out, err
	}
	defer cleanup()

	slug, err := s.createRow(ctx, in, &st)
	if err != nil {
		return out, err
	}
	out.Slug = slug
	out.RevisionNo = 1
	out.ModerationState = "pending"
	out.LifecycleState = "draft"

	// 从这里开始，数据库里已经有这一行了。任何失败都要把它清掉。
	if err := s.upload(ctx, slug, st); err != nil {
		s.compensate(ctx, slug, st)
		return SubmitResult{}, err
	}
	return out, nil
}

// stage 把上传包变成一份「即将上传的文件清单」。
func (s *Service) stage(ctx context.Context, in SubmitInput) (st staged, cleanup func(), err error) {
	dir, err := os.MkdirTemp("", "demo-submit-*")
	if err != nil {
		return st, func() {}, fmt.Errorf("demo: 创建暂存目录失败: %w", err)
	}
	cleanup = func() { _ = os.RemoveAll(dir) }
	st.dir = dir

	zr, zipErr := zip.OpenReader(in.PackagePath)
	if zipErr == nil {
		defer func() { _ = zr.Close() }()
		contentDir := filepath.Join(dir, "content")
		st.contentDir = contentDir
		stats, exErr := storage.ExtractZip(ctx, &zr.Reader, contentDir, s.limits)
		if exErr != nil {
			return st, cleanup, mapExtractError(exErr)
		}
		st.totalBytes = stats.Uncompressed
		if err := s.collect(contentDir, &st); err != nil {
			return st, cleanup, err
		}
		if st.entryPath == "" {
			return st, cleanup, fmt.Errorf("%w: 压缩包根目录必须有 index.html", ErrBadPackage)
		}
		// manifest 按「内容清单」算，不按 zip 字节算：同样的内容换个打包工具
		// 或换台机器的 mtime，zip 哈希就变了，而设计要求它与打包方式无关。
		st.manifest, err = canonicalManifest(contentDir, st.files)
		if err != nil {
			return st, cleanup, fmt.Errorf("demo: 计算清单哈希失败: %w", err)
		}
		// 原始包也存一份：解压逻辑以后修好了能就地重修，用户也能下载原件。
		srcCopy := filepath.Join(dir, "source.zip")
		if err := copyFile(in.PackagePath, srcCopy); err != nil {
			return st, cleanup, fmt.Errorf("demo: 保留原始包失败: %w", err)
		}
		st.sourcePath = srcCopy
		st.singleFile = ""
		return st, cleanup, nil
	}

	// 不是 zip：只接受单个 html / svg。按扩展名判断而不是嗅探内容——
	// 单文件作品的类型是用户声明的，嗅探反而会把「.svg 里带 HTML」这种情况判错。
	//
	// 扩展名只能从 PackageName 取：PackagePath 是 `os.CreateTemp` 的随机名，
	// 没有扩展名。（踩过：这里原来读 PackagePath，于是单文件投稿一律 415。）
	ext := strings.ToLower(filepath.Ext(in.PackageName))
	switch ext {
	case ".html", ".htm":
		st.singleFile = "html"
	case ".svg":
		st.singleFile = "svg"
	default:
		return st, cleanup, fmt.Errorf("%w: 只接受 zip 包或单个 .html / .svg 文件", ErrUnsupportedPackage)
	}
	name := "index" + ext
	if ext == ".htm" {
		name = "index.html" // 统一成 .html，避免预览时 MIME 判断分叉
	}
	st.contentDir = dir
	if err := copyFile(in.PackagePath, filepath.Join(dir, name)); err != nil {
		return st, cleanup, fmt.Errorf("demo: 暂存文件失败: %w", err)
	}
	info, err := os.Stat(filepath.Join(dir, name))
	if err != nil {
		return st, cleanup, fmt.Errorf("demo: 暂存文件失败: %w", err)
	}
	st.files = []string{name}
	st.entryPath = name
	st.totalBytes = info.Size()
	st.manifest, err = canonicalManifest(dir, st.files)
	if err != nil {
		return st, cleanup, fmt.Errorf("demo: 计算清单哈希失败: %w", err)
	}
	return st, cleanup, nil
}

// collect 列出解压目录下的全部普通文件（相对路径，`/` 分隔）。
func (s *Service) collect(root string, st *staged) error {
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		st.files = append(st.files, rel)
		if rel == "index.html" || rel == "index.htm" {
			st.entryPath = rel
		}
		return nil
	})
}

// createRow 在数据库里建作品与第一版，返回最终 slug。
//
// slug 冲突靠唯一索引 + 重试解决，不「先查再插」：先查后插有 TOCTOU 窗口，
// 两个并发投稿能同时通过检查。这里把 23505 当成「换一个 slug 再试」的信号。
func (s *Service) createRow(ctx context.Context, in SubmitInput, st *staged) (string, error) {
	base := slugify(in.Title)
	// 标题里没有可用 ASCII 时 slugify 返回空（纯中文标题就是这种情况）。
	// 这时不能用光秃秃的 "demo" 当第一版：第二个纯中文标题投稿会撞上它，
	// 之后全部退化成 "demo-xxxx"，反而把最想要的那个名字占了。
	// 所以从第一次尝试起就带后缀。
	needsSuffix := base == ""
	if base == "" {
		base = "demo"
	}

	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		slug := base
		if attempt > 0 || needsSuffix {
			suffix, err := randomSuffix(4)
			if err != nil {
				return "", fmt.Errorf("demo: 生成 slug 失败: %w", err)
			}
			slug = base + "-" + suffix
		}
		if len(slug) > maxSlugLen {
			slug = slug[:maxSlugLen]
		}

		err := s.inTx(ctx, func(tx pgx.Tx) error {
			q := s.q.WithTx(tx)
			d, err := q.CreateDemo(ctx, store.CreateDemoParams{
				Slug:        slug,
				Title:       text(strings.TrimSpace(in.Title)),
				Description: text(strings.TrimSpace(in.Description)),
				AuthorID:    int8(in.AuthorID),
			})
			if err != nil {
				return err
			}
			rev, err := q.CreateRevision(ctx, store.CreateRevisionParams{
				DemoID:       d.ID,
				Title:        text(strings.TrimSpace(in.Title)),
				Description:  text(strings.TrimSpace(in.Description)),
				DemoType:     in.DemoType,
				EntryPath:    text(st.entryPath),
				SingleFile:   text(st.singleFile),
				ManifestHash: text(st.manifest),
				FileCount:    int8(int64(len(st.files))),
				TotalBytes:   int8(st.totalBytes),
				CreatedBy:    int8(in.AuthorID),
			})
			if err != nil {
				return err
			}
			return q.SetCurrentRevision(ctx, store.SetCurrentRevisionParams{
				ID:                d.ID,
				CurrentRevisionID: int8(rev.ID),
			})
		})
		if err == nil {
			// 必须在**指针**上写：调用方要靠这个值决定要不要上传原始包。
			if st.sourcePath != "" {
				st.sourceKey = storage.SourceKey(slug, 1, "zip")
			}
			return slug, nil
		}
		if isForeignKeyViolation(err) {
			// 走到这里只可能是 demo_type 不存在（其余外键都由服务端自己填）。
			// 这是输入问题不是服务端故障，必须回 400 而不是 503。
			return "", fmt.Errorf("%w: 作品类型不存在", ErrInvalidInput)
		}
		if !isUniqueViolation(err) {
			return "", fmt.Errorf("demo: 落库失败: %w", err)
		}
		lastErr = err
		s.log.Warn("slug 冲突，换一个再试", "slug", slug, "attempt", attempt+1)
	}
	return "", fmt.Errorf("demo: 无法生成唯一 slug: %w", lastErr)
}

// upload 把暂存目录里的文件逐个写进存储。
func (s *Service) upload(ctx context.Context, slug string, st staged) error {
	if st.sourceKey != "" {
		if err := s.putFile(ctx, st.sourceKey, st.sourcePath); err != nil {
			return err
		}
	}
	for _, rel := range st.files {
		key := storage.FilesKey(slug, 1, rel)
		if err := s.putFile(ctx, key, filepath.Join(st.contentDir, filepath.FromSlash(rel))); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) putFile(ctx context.Context, key, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("demo: 打开暂存文件失败: %w", err)
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("demo: 读暂存文件信息失败: %w", err)
	}
	if err := s.files.Put(ctx, key, f, info.Size(), ""); err != nil {
		return fmt.Errorf("demo: 写入存储失败: %w", err)
	}
	return nil
}

// compensate 清掉「已落库但上传失败」的残留。
//
// 顺序：先删库再删文件。反过来的话，删文件成功、删库失败会留下一个指向空目录的作品；
// 而先删库最坏只是留几个孤儿对象，那些对象没有任何引用，可以靠将来的 GC 扫掉。
func (s *Service) compensate(ctx context.Context, slug string, st staged) {
	// 补偿用独立 context：请求 context 可能已经取消，那正是需要补偿的时候。
	cctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 先删库：库行是「作品存在」的唯一凭据。库删失败时文件还在，重跑补偿仍能清干净；
	// 反过来（先删文件再删库）一旦删库失败，就留下一条指向空目录的作品——
	// 它对外可见、点进去什么都没有，比孤儿对象难处理得多。
	//
	// 删 demos 一行即可：版本行由 fk_demo_revisions_demo_id76 的 ON DELETE CASCADE
	// 带走（迁移 0006），current_revision_id 是 SET NULL。
	if err := s.inTx(cctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		d, err := q.GetDemoBySlug(cctx, slug)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				// 库行已经不在了（补偿重跑），继续清文件。
				return nil
			}
			return err
		}
		return q.DeleteDemoForCleanup(cctx, d.ID)
	}); err != nil {
		// 库删失败：不删文件。留着文件，下一次补偿或 GC 还能按 slug 清掉，
		// 而此刻删文件只会把「库里有行、磁盘没文件」做实。
		s.log.Error("补偿删除作品失败，暂不删文件（可重跑补偿）", "slug", slug, "err", err)
		return
	}

	keys := make([]string, 0, len(st.files)+1)
	if st.sourceKey != "" {
		keys = append(keys, st.sourceKey)
	}
	for _, rel := range st.files {
		keys = append(keys, storage.FilesKey(slug, 1, rel))
	}
	for _, key := range keys {
		if err := s.files.Delete(cctx, key); err != nil {
			s.log.Error("清理孤儿对象失败", "key", key, "err", err)
		}
	}
}

func (s *Service) inTx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	if s.pool == nil {
		return errors.New("demo: 数据库未配置")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// mapExtractError 把存储层的解压错误翻成领域错误，避免把实现细节漏给用户。
func mapExtractError(err error) error {
	switch {
	case errors.Is(err, storage.ErrTooLarge), errors.Is(err, storage.ErrSuspiciousRatio),
		errors.Is(err, storage.ErrTooManyMembers):
		return fmt.Errorf("%w: %v", ErrBadPackage, err)
	case errors.Is(err, storage.ErrUnsafePath), errors.Is(err, storage.ErrEmptyArchive):
		return fmt.Errorf("%w: %v", ErrBadPackage, err)
	default:
		return fmt.Errorf("demo: 解压失败: %w", err)
	}
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

func randomSuffix(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// isForeignKeyViolation 判断是不是外键违反（23503）。
//
// 目前唯一来源是 demo_revisions.demo_type → demo_types：调用方传了格式合法但不存在的类型。
// 这是**输入问题**（400），不是服务端故障（503）——不翻的话它会落到
// writeDemoError 的 default 分支，而契约写的是 400「类型未知」。
func isForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503"
}

// text 把字符串包成可空列。空串按 NULL 处理：标题/描述是"内容列"，
// 按 DDL 约定 §7 不允许 DEFAULT ”，空串与 NULL 在语义上必须是同一件事。
func text(s string) pgtype.Text {
	if s == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: s, Valid: true}
}

func int8(v int64) pgtype.Int8 {
	return pgtype.Int8{Int64: v, Valid: true}
}
