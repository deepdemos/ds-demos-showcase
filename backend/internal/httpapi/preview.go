package httpapi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/identity"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/platform/storage"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// 预览托管：把已上传的作品文件按版本号吐出去。
//
// # 为什么不在 OpenAPI 契约里
//
// 这不是 JSON API：响应体是作品自己的 HTML/CSS/JS，内容类型与状态码由文件决定。
// 塞进契约会让「所有端点返回 JSON、错误有统一 ErrorBody」这条前提不成立，
// 而且生成的客户端会把一个二进制流当成对象去解析。
//
// # 为什么地址里带版本号
//
// `demo_revisions` 一旦创建不可修改（不变量 #1，触发器 trg_demo_revisions_immutable）。
// 版本化的 URL 因此可以永久缓存：同一个地址永远指向同一份字节。
// `/preview/{slug}/{path}` 不带版本的是「当前版本」别名，只能短缓存。
// 地址里的 `v{n}` 的 n 是 `demo_revisions.rev_no`（每个作品内自 1 递增），不是行 id。
//
// # 同源渲染的隔离
//
// 默认走 `/preview/...` 同源路径。作品 HTML 拿到的是不透明源（opaque origin），
// 靠两道独立机制保证：
//  1. 前端 iframe 的 sandbox 属性（不给 allow-same-origin）
//  2. 这里的 CSP `sandbox` 指令——用户直接在新标签页打开预览地址时也生效
//
// 第 2 条不是第 1 条的备份，是唯一在「直接访问」这条路径上生效的那一条。
// 代价是作品页里 localStorage 会抛异常、不能读同源其它作品的存储——
// 这正是设计要的（docs/多人联机方案调研.md:146 记录的预览源共享问题）。
const (
	// previewReadTimeout 是读一个文件的上限。作品文件可能很大（封面图、字体），
	// 但都在本地磁盘，10 秒足够；卡住说明盘有问题，不该拖住整个请求。
	previewReadTimeout = 10 * time.Second

	// previewMaxHTMLBytes 是需要注入 <base> 时读进内存的上限。
	// 超过这个体积的 HTML **不注入 <base>**，但文件其余部分照常原样补发：
	// 把几 MB 的单页应用读进内存再复制一份，收益抵不上内存峰值。
	// 代价是超大 HTML 里的相对路径不会被改写——那种作品本来就该用绝对路径。
	previewMaxHTMLBytes = 8 << 20

	// previewCSP 见上面「同源渲染的隔离」。
	// 不给 allow-same-origin 是刻意的：给了它，sandbox 就等于没有。
	previewCSP = "sandbox allow-scripts allow-forms allow-popups allow-modals allow-pointer-lock"
)

// Preview 托管作品文件。
//
// 它不实现 api.ServerInterface：预览不是契约端点（理由见上方注释），
// 由 router.go 直接挂到 chi 上。
type Preview struct {
	queries  *store.Queries
	files    storage.Store
	identity *identity.Service
}

// NewPreview 组装预览处理器。files 为 nil 表示存储未接线，所有预览返回 404。
func NewPreview(queries *store.Queries, files storage.Store, identitySvc *identity.Service) *Preview {
	return &Preview{queries: queries, files: files, identity: identitySvc}
}

// ServeHTTP 处理 `/preview/{slug}/{rest...}`。
//
// rest 的第一段如果是 `v<数字>` 就当版本号，否则当作「当前版本 + 路径」。
// 只注册一条路由而不是两条：chi 的两条模式会让 `{slug}`/`{rev}` 这类同位置参数
// 的优先级变得依赖注册顺序，而这里的分支本来就只有一行。
func (p *Preview) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		// `Allow` 必须留在响应头里：405 的语义一半在这个头（RFC 9110 §15.5.6）。
		w.Header().Set("Allow", "GET, HEAD")
		// 但正文不该是 `http.Error` 的一行纯文本——它同样会出现在 iframe 里，
		// 与另外三条失败页（404 / 500 / 502）并排时显得像坏掉了。
		previewError(w, r, http.StatusMethodNotAllowed, previewMethodDetail)
		return
	}
	if p.queries == nil || p.files == nil {
		previewError(w, r, http.StatusNotFound, previewMissingDetail)
		return
	}

	slug := chi.URLParam(r, "slug")
	rest := strings.TrimPrefix(chi.URLParam(r, "*"), "/")

	rev, rel, versioned := splitPreviewPath(rest)

	ctx, cancel := context.WithTimeout(r.Context(), previewReadTimeout)
	defer cancel()

	row, err := p.queries.GetDemoForPreview(ctx, slug)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.Error("preview: 查作品失败", "slug", slug, "err", err)
		}
		previewError(w, r, http.StatusNotFound, previewMissingDetail)
		return
	}

	// 擦除与软删对外一律不存在。归档/移除不在这里拒——它们要能显示墓碑页，
	// 而墓碑页由前端按 API 返回的 lifecycle_state 渲染，不需要读预览文件。
	//
	// **单个版本的擦除也要拦**：demo_revisions 的 erased_at 是给「只处理这一版」留的
	// （数据生命周期 §6 的文件轴）。只判 demos 的话，擦掉一版之后磁盘上的
	// `demos/{slug}/r{n}/` 还在（没有 purge 代码），预览照常 200。
	if row.ErasedAt.Valid || row.DeletedAt.Valid {
		previewError(w, r, http.StatusNotFound, previewMissingDetail)
		return
	}
	// 版本级的擦除：当前版本与对外版本各自的行状态都能从作品行上读出来，
	// 所以这两种都要拦；更早的历史版本查不到行状态，磁盘上的 r{n} 目录仍在。
	if rev > 0 && int32(rev) == row.CurrentRevNo {
		if row.RevisionErasedAt.Valid || row.RevisionDeletedAt.Valid {
			previewError(w, r, http.StatusNotFound, previewMissingDetail)
			return
		}
	}
	if rev > 0 && int32(rev) == row.PublishedRevNo {
		if row.PublishedErasedAt.Valid || row.PublishedDeletedAt.Valid {
			previewError(w, r, http.StatusNotFound, previewMissingDetail)
			return
		}
	}

	// 对外可见 = 已发布 **且** 审过 **且** 渠道不是 hidden。
	//
	// 前两条缺一都只有作者与审核员能看，因为「审核通过」与「发布」是两次独立的
	// 状态跃迁（设计 §7.4）。第三条是本轮补的：之前预览完全不看渠道，
	// 于是 hidden 的作品「从列表里藏起来了，但知道直链的人仍然能看完整内容」——
	// 下载端点在上一轮已经判了渠道，预览没判，两处口径不一致。
	//
	// 判定放在 `published` 里而不是加一道独立的 404：缓存头
	// （`cacheControlOf(published, versioned)`）与它共用同一个布尔，
	// 分开写会出现「按不可见处理但给了 immutable 长缓存」这种自相矛盾的组合。
	// 对外可见 = 已发布 **且有对外版本** **且** 渠道不是 hidden。
	//
	// 中间那一条原来是「审核态必须是 approved」（迁移 0014）。换掉的理由与前几处
	// 读路径相同：审核态从此描述**当前版本**，作者改稿后它是 pending，
	// 而对外的内容仍是上一版、照常在服务。继续判审核态会让作者一提交更新，
	// 线上作品的预览与下载同时消失。
	published := row.LifecycleState == "published" &&
		row.HasPublishedRevision &&
		row.HasVisibleChannel
	if !published && !p.mayPreviewDraft(ctx, r, row.AuthorSub) {
		// 作者视图不受渠道影响：作者要能在发布到 hidden 之后仍然检查自己的作品，
		// 审核员同理（他们看的是「还没对外」的东西，本来就该绕过渠道判定）。
		previewError(w, r, http.StatusNotFound, previewMissingDetail)
		return
	}

	// 没指定版本时按「谁在看」选：作者与审核员看当前版本（含未过审的改动），
	// 其他人看对外版本。**顺序不能反**——先判作者再判对外，
	// 否则作者改完稿打开自己的作品看到的还是旧版。
	//
	// 变量叫 privilegedView 而不是 authorView：mayPreviewDraft 对**作者与持
	// demo.review 的审核员**都返回 true。按作者命名会让下面那句
	// `if rev > 0 && !privilegedView` 读起来像「审核员被挡」，实际不是。
	privilegedView := p.mayPreviewDraft(ctx, r, row.AuthorSub)
	if rev == 0 {
		if privilegedView && row.CurrentRevNo > 0 {
			rev = int(row.CurrentRevNo)
		} else {
			rev = int(row.PublishedRevNo)
		}
	}
	if rev <= 0 {
		previewError(w, r, http.StatusNotFound, previewMissingDetail)
		return
	}
	// 明确指定版本号时，非作者**只能看对外版本**。
	//
	// 不加这道判定的话，作者改稿期间任何人都能拿 `/preview/{slug}/v{当前版本}/`
	// 看到还没过审的内容——上面那个 `privilegedView` 只决定「rev==0 时选哪一版」，
	// 而路径里的版本号是调用方自己填的。作者的「预览新版」按钮走的正是显式版本，
	// 所以不能一刀切禁掉，只能对非作者收紧。
	if rev > 0 && !privilegedView && int32(rev) != row.PublishedRevNo {
		previewError(w, r, http.StatusNotFound, previewMissingDetail)
		return
	}
	if rel == "" {
		// 没有文件名时**不猜**入口，而是跳过去。
		//
		// 以前这里直接回字节，入口靠一段代码推——推错了就是「投稿成功、预览页 404」
		// （单文件作品的原件叫 index.svg，而代码推的是 index.html）。
		// 改成 302 之后，「猜文件名」这个动作消失了：入口只从这一版的
		// `entry_path` 取一次，结果直接摆在地址里。
		//
		// 跳过去而不是回字节，还顺手解决两件事：
		//   - 地址可读：`/preview/111/v1/index.svg` 一眼看得出读的是哪个文件；
		//   - 缓存：带版本号的地址永久不变，可以给 immutable。
		//
		// 读的是**这一版**的入口：作者在 `/preview/{slug}/v{n}/` 里看的就是第 n 版。
		entry := ""
		if privilegedView && int32(rev) == row.CurrentRevNo {
			entry = row.CurrentEntryPath
		} else if int32(rev) == row.PublishedRevNo {
			entry = row.PublishedEntryPath
		}
		if entry == "" {
			// 这一版没记入口：数据坏了（写入侧一定写），不要用默认值掩盖它。
			slog.Error("preview: 版本没有记录入口文件", "slug", slug, "rev", rev)
			previewError(w, r, http.StatusInternalServerError, previewMissingEntryDetail)
			return
		}
		target := fmt.Sprintf("/preview/%s/v%d/%s", url.PathEscape(slug), rev, entry)
		// 不带版本号的那种跳转目标会随改稿变化，只能 no-store；
		// 带版本号的目标是确定值，交给下游那次请求给 immutable。
		w.Header().Set("Cache-Control", "no-store")
		http.Redirect(w, r, target, http.StatusFound)
		return
	}

	key, err := p.resolveKey(ctx, slug, rev, rel)
	if err != nil {
		previewError(w, r, http.StatusNotFound, previewMissingDetail)
		return
	}
	f, err := p.files.Get(ctx, key)
	if err != nil {
		if !errors.Is(err, storage.ErrNotFound) && !errors.Is(err, storage.ErrUnsafeKey) {
			slog.Error("preview: 读文件失败", "key", key, "err", err)
		}
		previewError(w, r, http.StatusNotFound, previewMissingDetail)
		return
	}
	defer f.Close()

	ct := contentTypeOf(rel)
	w.Header().Set("Content-Type", ct)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", cacheControlOf(published, versioned))
	if needsSandbox(ct) {
		w.Header().Set("Content-Security-Policy", previewCSP)
	}

	// 只有需要注入 <base> 的 HTML 才整体读进内存。
	if strings.HasPrefix(ct, "text/html") {
		body, err := io.ReadAll(io.LimitReader(f, previewMaxHTMLBytes+1))
		if err != nil {
			slog.Error("preview: 读 HTML 失败", "key", key, "err", err)
			previewError(w, r, http.StatusBadGateway, previewReadFailedDetail)
			return
		}
		// 超过上限：不注入 <base>，但**必须把剩下的字节原样送出去**。
		// 只写 head 那 8 MiB 会让响应看起来完整（Content-Length 也是截断后的长度），
		// 客户端没有任何办法发现少了东西。
		var rest io.Reader
		if len(body) > previewMaxHTMLBytes {
			rest = f
		} else {
			body = injectBase(body, previewBase(slug, rev))
		}
		w.WriteHeader(http.StatusOK)
		if r.Method != http.MethodHead {
			_, _ = w.Write(body)
			if rest != nil {
				_, _ = io.Copy(w, rest)
			}
		}
		return
	}

	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = io.Copy(w, f)
	}
}

// resolveKey 决定这个请求去存储里读哪个 key。
//
// 两条路：
//
//  1. **清单优先**（迁移后的新数据）：`demo_revision_files` 里有一行
//     `(版本, 相对路径) → 内容地址`，用它反查出 `blobs.storage_key`。
//  2. **目录回退**（迁移前的旧数据）：那一版没有清单行，文件仍按
//     `demos/{slug}/r{n}/{rel}` 铺在盘上。
//
// 为什么不在发布时就把旧数据搬进 blob 表：搬运要读全量文件、算哈希、重写，
// 一旦中途失败还得回滚；而旧数据的**读取路径已经能工作**，搬运的收益只有磁盘。
// 等它们自然被更新覆盖（作者下次改稿就走新路）是更便宜的选择。
//
// 清单命中但 blob 行缺失（数据库被手工改过）时**回退到目录**而不是直接 404：
// 文件其实可能还在老地方，多试一次只花一次 Stat。
func (p *Preview) resolveKey(ctx context.Context, slug string, rev int, rel string) (string, error) {
	row, err := p.queries.GetRevisionFile(ctx, store.GetRevisionFileParams{
		Slug:  slug,
		RevNo: int32(rev),
		Path:  rel,
	})
	switch {
	case err == nil:
		return row.StorageKey, nil
	case errors.Is(err, pgx.ErrNoRows):
		// 没有清单行：旧数据。走目录布局。
	case err != nil:
		slog.Error("preview: 查清单失败", "slug", slug, "rev", rev, "path", rel, "err", err)
	}
	return storage.FilesKey(slug, rev, rel), nil
}

// mayPreviewDraft 判断这次请求能不能看未发布的内容：作者本人，或持有审核权的角色。
//
// 读会话或查权限失败一律当「不能看」——这里放行等于把所有人的草稿公开。
func (p *Preview) mayPreviewDraft(ctx context.Context, r *http.Request, authorSub string) bool {
	if p.identity == nil {
		return false
	}
	sid := readSessionCookie(r)
	if sid == "" {
		return false
	}
	me, err := p.identity.Me(ctx, sid)
	if err != nil {
		return false
	}
	if authorSub != "" && me.Sub == authorSub {
		return true
	}
	ok, err := p.queries.HasPermission(ctx, store.HasPermissionParams{
		Sub:           me.Sub,
		PermissionKey: "demo.review",
	})
	if err != nil {
		slog.Error("preview: 查权限失败", "sub", me.Sub, "err", err)
		return false
	}
	return ok
}

// splitPreviewPath 拆出「版本号 / 相对路径 / 是否版本化」。
//
// `v12/page.html`  → (12, `page.html`, true)
// `page.html`      → (0,  `page.html`, false)
// `v12`            → (12, "",          true)
func splitPreviewPath(rest string) (rev int, rel string, versioned bool) {
	if rest == "" {
		return 0, "", false
	}
	head, tail, hasTail := strings.Cut(rest, "/")
	if n, ok := parseRevSegment(head); ok {
		if hasTail {
			return n, tail, true
		}
		return n, "", true
	}
	return 0, rest, false
}

// parseRevSegment 认 `v12` 这种段（`v` + rev_no，与 docs/Demo域设计.md 的
// `/preview/{slug}/v{version}/{path}` 一致）。不是版本段就返回 false，交给路径处理。
func parseRevSegment(seg string) (int, bool) {
	if len(seg) < 2 || seg[0] != 'v' {
		return 0, false
	}
	n, err := strconv.Atoi(seg[1:])
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

// previewBase 是注入进 HTML 的 <base href>。
//
// 作品包里的相对路径（`./style.css`、`img/a.png`）原本相对于「解压后的目录」，
// 现在页面地址是 `/preview/{slug}/v{rev}/index.html`，相对路径恰好落在同一个目录，
// 本来就能用。注入 <base> 的理由是**子目录里的页面**（`sub/about.html`）：
// 没有 base 时 `../style.css` 会解析到 `/preview/{slug}/style.css`，越出正确位置。
func previewBase(slug string, rev int) string {
	return "/preview/" + slug + "/v" + strconv.Itoa(rev) + "/"
}

// contentTypeOf 按扩展名给 Content-Type。
//
// mime.TypeByExtension 在不同系统读的注册表不同（Windows 上 .js 可能给
// text/plain），所以常见类型写死，退路才用 mime。
// needsSandbox 判断这个 Content-Type 是否需要 CSP sandbox。
//
// 不是「是不是 HTML」——**SVG 与 XHTML 作为顶层文档打开时会执行脚本**，
// 而它们运行在主站源上。只给 text/html 加 sandbox 的话，
// 作者提交一个带 <script> 的 .svg，审核员在队列里点预览，
// 脚本就在审核员的会话里跑起来（HttpOnly 挡不住，因为脚本不需要读 cookie，
// 它就在同一个源里直接发请求）。
//
// 白名单之外一律不加：图片、字体、wasm、纯文本不会执行脚本，
// 给它们加 sandbox 反而可能影响正常的资源加载。
func needsSandbox(ct string) bool {
	base := strings.ToLower(strings.TrimSpace(strings.SplitN(ct, ";", 2)[0]))
	switch base {
	case "text/html", "image/svg+xml", "application/xhtml+xml",
		"text/xml", "application/xml", "application/rss+xml",
		"application/atom+xml", "text/xsl", "application/xslt+xml":
		return true
	}
	// 兜底：任何 +xml 后缀的文档类型都可能带脚本（XSLT / XML 数据岛）。
	return strings.HasSuffix(base, "+xml") || strings.HasSuffix(base, "/xml")
}

func contentTypeOf(rel string) string {
	switch strings.ToLower(path.Ext(rel)) {
	case ".html", ".htm":
		return "text/html; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".js", ".mjs":
		return "text/javascript; charset=utf-8"
	case ".json":
		return "application/json; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".ico":
		return "image/x-icon"
	case ".woff2":
		return "font/woff2"
	case ".woff":
		return "font/woff"
	case ".ttf":
		return "font/ttf"
	case ".wasm":
		return "application/wasm"
	case ".txt", ".md":
		return "text/plain; charset=utf-8"
	}
	if ct := mime.TypeByExtension(path.Ext(rel)); ct != "" {
		return ct
	}
	return "application/octet-stream"
}

// cacheControlOf 决定缓存策略。
//
// 版本化 + 已发布 = 内容永不改变 → 一年 immutable。
// 其余一律不让中间层缓存：草稿会变，当前版本别名也会变。
func cacheControlOf(published, versioned bool) string {
	if published && versioned {
		return "public, max-age=31536000, immutable"
	}
	if published {
		return "public, max-age=60"
	}
	return "private, no-store"
}

// injectBase 在 <head> 之后插入 <base href>。
//
// 找不到 <head> 就退回插在 <html ...> 之后；再找不到就放最前面。
// 不做完整 HTML 解析：那要引 golang.org/x/net/html，为一个字符串插入不值得。
func injectBase(doc []byte, base string) []byte {
	tag := []byte(`<base href="` + base + `">`)
	lower := bytes.ToLower(doc)

	if i := bytes.Index(lower, []byte("<head")); i >= 0 {
		if j := bytes.IndexByte(doc[i:], '>'); j >= 0 {
			at := i + j + 1
			return splice(doc, at, tag)
		}
	}
	if i := bytes.Index(lower, []byte("<html")); i >= 0 {
		if j := bytes.IndexByte(doc[i:], '>'); j >= 0 {
			at := i + j + 1
			return splice(doc, at, tag)
		}
	}
	// 找不到 <head> / <html>：按 HTML 规范 doctype 必须是第一个 token，
	// 插在它前面会让浏览器进 quirks mode（盒模型、行高等全变）。
	// 有 doctype 就插在它之后。
	if i := bytes.Index(lower, []byte("<!doctype")); i == 0 {
		if j := bytes.IndexByte(doc, '>'); j >= 0 {
			return splice(doc, j+1, tag)
		}
	}
	return splice(doc, 0, tag)
}

func splice(doc []byte, at int, ins []byte) []byte {
	out := make([]byte, 0, len(doc)+len(ins))
	out = append(out, doc[:at]...)
	out = append(out, ins...)
	out = append(out, doc[at:]...)
	return out
}
