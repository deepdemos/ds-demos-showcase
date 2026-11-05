package httpapi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/platform/storage"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// 作品下载：把当初上传的原件吐回去。
//
// # 为什么不在 OpenAPI 契约里
//
// 和预览同一个理由：响应体是 zip 或 HTML 字节，不是 JSON。塞进契约会让生成的
// 客户端把一个二进制流当成对象解析，而且「所有端点返回 JSON、错误有统一 ErrorBody」
// 这条前提不成立。失败时仍然走纯文本 + 状态码，与预览一致。
//
// # 为什么下载的是原件而不是「重新打包当前版本」
//
// 投稿时已经把原包按版本存成 `demos/{slug}/r{rev}/source.zip`
// （`storage.SourceKey`，注释写明「保留原件是为了重新解压、以及『下载原件』这个用户动作」）。
// 重新打包会引入两个新问题：解压出的目录再压一遍字节不同（DEMO-039 要求
// 「解压后 manifest 与 A 的 revision 一致」——同一份字节天然满足），
// 而且要多一份临时目录与一次全量读盘。
//
// 单文件作品（html / svg）没有 source.zip，原件就是解压目录里的 index.*，
// 所以两条路都要能走。
//
// # 可见性口径
//
// 已发布 + 已审过 + 渠道里有一条`listed` 或 `unlisted`。
// **与列表不同**：`unlisted` 可以直链下载（它是「不上列表但知道地址就能看」的档位），
// `hidden` 一律拒绝。这条判定单独写在这里，与 `GetDemoForDownload` 的
// `has_visible_channel` 是同一件事的两处写法——所以只在这一处算，别在别处再算一遍。
const downloadTimeout = 30 * time.Second

// countTimeout 是「传完之后记一次下载」的预算。它跟传输时长无关，
// 所以单独给一个很短的值——这一步只是一条 UPSERT。
const countTimeout = 5 * time.Second

// Download 托管 `/download/{slug}`。
//
// 不实现 api.ServerInterface：不是契约端点（理由见上方注释），由 router.go 直接挂到 chi。
type Download struct {
	queries *store.Queries
	files   storage.Store
}

// NewDownload 组装下载处理器。files 为 nil 表示存储未接线，所有下载返回 404。
func NewDownload(queries *store.Queries, files storage.Store) *Download {
	return &Download{queries: queries, files: files}
}

// ServeHTTP 处理 `/download/{slug}`。
func (d *Download) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "只支持 GET", http.StatusMethodNotAllowed)
		return
	}
	if d.queries == nil || d.files == nil {
		http.NotFound(w, r)
		return
	}

	slug := chi.URLParam(r, "slug")

	ctx, cancel := context.WithTimeout(r.Context(), downloadTimeout)
	defer cancel()

	row, err := d.queries.GetDemoForDownload(ctx, slug)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.Error("download: 查作品失败", "slug", slug, "err", err)
		}
		http.NotFound(w, r)
		return
	}

	// 擦除与软删对外一律不存在，与预览同一口径。
	if row.ErasedAt.Valid || row.DeletedAt.Valid {
		http.NotFound(w, r)
		return
	}
	// 当前版本被单独擦除：磁盘上文件还在（没有 purge 代码），必须拦住。
	if row.RevisionErasedAt.Valid || row.RevisionDeletedAt.Valid {
		http.NotFound(w, r)
		return
	}

	// 对外可见 = 已发布 且 有对外版本 且 渠道不是 hidden。
	//
	// **不判审核态**（迁移 0014）：审核态描述的是「当前版本」，复审期间它是 pending，
	// 但对外内容仍是上一版、照常在服务。判审核态会让「作者改了一版」直接把线上作品
	// 从下载路径上摘掉。
	//
	// 下载不区分「作者本人」——草稿不该出现在任何下载路径上，
	// 作者要看自己的作品走 /preview（那里有作者视图）。
	if row.LifecycleState != "published" || !row.HasVisibleChannel {
		http.NotFound(w, r)
		return
	}
	if row.PublishedRevNo <= 0 {
		http.NotFound(w, r)
		return
	}

	// 外链作品没有本站的文件可给。DEMO-038 要的是「外链作品只给外链，不给假的本站包」。
	// 现在 link 类型根本投不进来（stage() 只认 zip 或单文件 html/svg），
	// 这个分支是给「以后能投外链」留的，不是当前可达状态。
	if row.DemoType == "link" {
		http.Error(w, "外链作品请访问原地址", http.StatusBadRequest)
		return
	}

	name, key := downloadTarget(row.Slug, int(row.PublishedRevNo), row.SingleFile)

	f, err := d.files.Get(ctx, key)
	if err != nil {
		if !errors.Is(err, storage.ErrNotFound) && !errors.Is(err, storage.ErrUnsafeKey) {
			slog.Error("download: 读文件失败", "key", key, "err", err)
		}
		// 文件缺失是 404 而不是空文件：DEMO-040 要求「明确失败」。
		http.NotFound(w, r)
		return
	}
	defer func() { _ = f.Close() }()

	// 先问大小再发头：`Content-Length` 让浏览器能画进度条、能在断线后判断是不是传完了。
	//
	// 以前这里没有长度，传输靠 chunked 编码收尾——两边都不知道「还剩多少」，
	// 一个 30 MB 的包传到一半断线，客户端看到的是一个正常结束的短文件。
	// Stat 失败不该拦下载（文件可能可读但元数据查不到），所以只降级不报错。
	setDownloadHeaders(w, name)
	if size, err := d.files.Stat(ctx, key); err == nil && size >= 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	} else if err != nil {
		slog.Warn("download: 取文件大小失败，改按分块传输", "key", key, "err", err)
	}

	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	if _, err := io.Copy(w, f); err != nil {
		// 响应头已经发出去了，只能记日志。客户端会看到被截断的传输。
		slog.Error("download: 写响应失败", "slug", slug, "err", err)
		return
	}

	// 计数放在字节真正写完之后：半路断掉的传输不该算一次下载。
	//
	// **必须换一个 context**：上面那个 ctx 是给「查库 + 打开文件」用的 30 秒预算，
	// 而它从进函数起就在走表——传一个 300 MB 的包要几分钟，等 io.Copy 回来它早过期了，
	// 于是每一次慢下载都会在「最后一步」失败，计数器永远停在原地。
	// 客户端断开时 r.Context() 会被取消，但「文件已经传完」这件事仍然成立，
	// 所以这里用 WithoutCancel 接着 r.Context() 的取消语义之外的部分。
	countCtx, cancelCount := context.WithTimeout(context.WithoutCancel(r.Context()), countTimeout)
	defer cancelCount()

	// 失败只记日志——用户已经拿到文件了，不该因为计数失败给他一个错误页。
	if err := d.queries.BumpDownloadCount(countCtx, row.ID); err != nil {
		slog.Error("download: 计数失败", "slug", slug, "err", err)
	}
}

// downloadTarget 决定「下载哪个文件、叫什么名字」。
//
// 单文件作品（`single_file` 非空）下载解压目录里的 index.*；
// 多文件作品下载原包 source.zip。
//
// 文件名带作品 slug 与版本号：浏览器把同一个作品的不同版本下到同一个目录时
// 不会互相覆盖，用户也能一眼看出自己拿的是哪一版。
func downloadTarget(slug string, revNo int, singleFile string) (name string, key string) {
	base := slug + "-v" + strconv.Itoa(revNo)
	if singleFile != "" {
		ext := "." + singleFile
		return base + ext, storage.FilesKey(slug, revNo, "index"+ext)
	}
	return base + ".zip", storage.SourceKey(slug, revNo, "zip")
}

// setDownloadHeaders 设置下载响应头。
//
// `Content-Disposition: attachment` 是下载与预览的分界线：少了它，浏览器会
// 把 zip 当页面渲染、把 HTML 当页面打开——后者更糟，作品里的脚本会跑在**主站源**上
// （预览那条路径靠 CSP sandbox 挡着，下载这条没有）。
//
// `filename*` 用 RFC 5987 编码：标题里带中文时 `filename="..."` 会被部分浏览器
// 按 latin-1 解码成乱码。这里只用 slug（ASCII），所以两种写法都给，兼容面最大。
//
// `Cache-Control` 给 `no-store`：下载会计数，缓存命中就少记一次；
// 而且下载是低频动作，省这点带宽不值当。
func setDownloadHeaders(w http.ResponseWriter, name string) {
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
}
