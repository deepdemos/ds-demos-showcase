package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 预览失败页的存在理由：`/preview/{slug}/` 渲染在详情页的 iframe 里，
// 前端跨源读不到 iframe 内部状态，所以「给人看的一页」只能由服务端给。
//
// 这组用例刻意不碰数据库以外的世界：404 走真实路由与真实身份服务（未配库的
// 预览在没有 queries 时就返回 404），500/502 用直接构造的 Preview 打。

func TestPreviewErrorPageIsChineseHTML(t *testing.T) {
	p := newPreview(t, integrationPool(t), t.TempDir())
	rec := previewGet(p, "/preview/does-not-exist-xyz/", "")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("不存在的作品 = %d，想要 404", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("Content-Type = %q，想要 text/html", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("Cache-Control = %q，想要 no-store", cc)
	}
	if csp := rec.Header().Get("Content-Security-Policy"); !strings.HasPrefix(csp, "sandbox") {
		t.Fatalf("CSP = %q，想要以 sandbox 开头（与正常预览一致）", csp)
	}
	body := rec.Body.String()
	if !strings.Contains(body, previewErrorTitle) {
		t.Fatalf("正文里没有「%s」：%s", previewErrorTitle, body)
	}
	if !strings.Contains(body, previewMissingDetail) {
		t.Fatalf("正文里没有 404 的说明")
	}
	if !strings.Contains(body, `lang="zh-CN"`) {
		t.Fatalf("页面没有声明语言")
	}
	// 纯文本时代的痕迹不该再出现。
	if strings.Contains(body, "404 page not found") {
		t.Fatalf("仍是 net/http 的纯文本页面")
	}
}

// TestPreviewErrorPageHasNoRequestContent：页面里不许出现请求里的任何字符串。
// 用一个带标签的 slug 打进去，正文里连它的片段都不该有。
func TestPreviewErrorPageHasNoRequestContent(t *testing.T) {
	p := newPreview(t, integrationPool(t), t.TempDir())
	probe := `xss-<script>alert(1)</script>`
	rec := previewGet(p, "/preview/"+strings.ReplaceAll(probe, "/", "")+"/", "")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("= %d，想要 404", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "<script>") {
		t.Fatalf("正文里出现了请求里的 <script> —— 失败页有注入面")
	}
	if strings.Contains(body, "alert(1)") {
		t.Fatalf("正文里出现了请求里的字符串")
	}
	if strings.Contains(body, "xss-") {
		t.Fatalf("正文里出现了 slug")
	}
}

// TestPreviewErrorPageHeadHasNoBody：HEAD 与 GET 同码、无 body。
func TestPreviewErrorPageHeadHasNoBody(t *testing.T) {
	p := newPreview(t, integrationPool(t), t.TempDir())
	req := httptest.NewRequest(http.MethodHead, "/preview/does-not-exist-xyz/", nil)
	rec := httptest.NewRecorder()
	NewRouter(&stub{}, p, nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("HEAD = %d，想要 404", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("HEAD 带了 %d 字节 body", rec.Body.Len())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("HEAD 的 Content-Type = %q，想要 text/html", ct)
	}
}

// TestPreviewErrorMissingEntryIs500：这一版的 `entry_path` 为空是**数据问题**
// （写入侧一定会写），不能为了好看改成 404——404 会让作者以为作品被删了。
//
// 注意：**文件被删走的是 404 而不是 500**（`storage.ErrNotFound` 的既有语义），
// 那条不在本用例里，也不该为了「看起来统一」去改状态码。
func TestPreviewErrorMissingEntryIs500(t *testing.T) {
	pool := integrationPool(t)
	dir := t.TempDir()
	seedPreviewWorld(t, pool, dir)

	// 入口在库里是空串 = 数据坏了（写入侧一定写）。`demo_revisions` 有不可变
	// 触发器（只允许改 deleted_at），所以这条数据只能**插**出来，不能 UPDATE 出来。
	ctx := context.Background()
	var authorUserID int64
	if err := pool.QueryRow(ctx, `SELECT id FROM users WHERE sub = $1`, pvAuthorSub).Scan(&authorUserID); err != nil {
		t.Fatalf("取作者 users.id 失败: %v", err)
	}
	var demoID, revID int64
	if err := pool.QueryRow(ctx, `
INSERT INTO demos (slug, title, description, moderation_state, lifecycle_state, author_id)
VALUES ('pv-no-entry', '标题 pv-no-entry', '简介', 'approved', 'published', $1)
RETURNING id`, authorUserID).Scan(&demoID); err != nil {
		t.Fatalf("插入 pv-no-entry 失败: %v", err)
	}
	if err := pool.QueryRow(ctx, `
INSERT INTO demo_revisions (demo_id, rev_no, source, manifest_hash, file_count, entry_path, single_file)
VALUES ($1, 1, 'upload', repeat('b', 64), 0, '', NULL)
RETURNING id`, demoID).Scan(&revID); err != nil {
		t.Fatalf("插入 pv-no-entry 的版本失败: %v", err)
	}
	if _, err := pool.Exec(ctx, `
UPDATE demos SET current_revision_id = $1, published_revision_id = $1 WHERE id = $2`,
		revID, demoID); err != nil {
		t.Fatalf("设置 pv-no-entry 的版本失败: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO demo_channels (demo_id, channel, visibility, published_at)
VALUES ($1, 'deep', 'listed', now())`, demoID); err != nil {
		t.Fatalf("建 pv-no-entry 的渠道失败: %v", err)
	}

	p := newPreview(t, pool, dir)
	rec := previewGet(p, "/preview/pv-no-entry/", "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("缺入口文件 = %d，想要 500；响应体：%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("Content-Type = %q，想要 text/html", ct)
	}
	if !strings.Contains(rec.Body.String(), previewMissingEntryDetail) {
		t.Fatalf("正文里没有 500 的说明：%s", rec.Body.String())
	}
}

// 405 也是给人看的：这一条以前回的是 `http.Error` 的一行纯文本，与另外三条
// 失败页并排时像坏掉了。状态码与 `Allow` 头都不能丢——405 的语义一半在头上。
func TestPreviewErrorMethodNotAllowedIsHTML(t *testing.T) {
	p := newPreview(t, integrationPool(t), t.TempDir())
	req := httptest.NewRequest(http.MethodPost, "/preview/does-not-exist-xyz/", nil)
	rec := httptest.NewRecorder()
	NewRouter(&stub{}, p, nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST 预览 = %d，想要 405", rec.Code)
	}
	if got := rec.Header().Get("Allow"); got != "GET, HEAD" {
		t.Fatalf("Allow = %q，想要 GET, HEAD", got)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("Content-Type = %q，想要 text/html", ct)
	}
	if csp := rec.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "sandbox") {
		t.Fatalf("CSP = %q，想要带 sandbox", csp)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "这个作品现在打不开") {
		t.Fatalf("正文没有兜底页标题：%q", body)
	}
}
