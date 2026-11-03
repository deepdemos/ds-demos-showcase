package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/platform/storage"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// 预览的可见性判定是「错了会静默出错」的那一类：放得太开等于公开草稿，
// 收得太紧等于审核员看不见东西。所以每条分支都钉一个用例。

const (
	pvAuthorSub = "ITPREVIEWAUTHOR000000000AA"
	pvModSub    = "ITPREVIEWMOD000000000000AA"
	pvAuthorSID = "it-preview-author-session"
	pvModSID    = "it-preview-mod-session"
)

// seedPreviewWorld 造两套人（作者 / 审核员）+ 四个作品（草稿 / 已发布 / 已归档 / 已擦除），
// 并把每个作品的 r1 内容写到磁盘上（磁盘用 r{n}，URL 用 v{n}）。
func seedPreviewWorld(t *testing.T, pool *pgxpool.Pool, dir string) {
	t.Helper()
	ctx := context.Background()

	for _, q := range []string{
		"DELETE FROM demo_channels", "DELETE FROM demo_revisions", "DELETE FROM demos",
		"DELETE FROM web_sessions", "DELETE FROM users", "DELETE FROM accounts",
	} {
		if _, err := pool.Exec(ctx, q); err != nil {
			t.Fatalf("%s 失败: %v", q, err)
		}
	}

	// 作者：普通用户（role=user，没有 demo.review）
	mkAccount(t, pool, pvAuthorSub, pvAuthorSID, "it-preview-author", "user")
	// 审核员：role=moderator（role_permissions 里绑了 demo.review）
	mkAccount(t, pool, pvModSub, pvModSID, "it-preview-mod", "moderator")

	var authorUserID int64
	if err := pool.QueryRow(ctx, `SELECT id FROM users WHERE sub = $1`, pvAuthorSub).Scan(&authorUserID); err != nil {
		t.Fatalf("取作者 users.id 失败: %v", err)
	}

	type demoSpec struct {
		slug      string
		lifecycle string
		erased    bool
	}
	specs := []demoSpec{
		{"pv-draft", "draft", false},
		{"pv-published", "published", false},
		{"pv-archived", "archived", false},
		{"pv-erased", "published", true},
	}

	for _, s := range specs {
		title, desc := "标题 "+s.slug, "简介"
		if s.erased {
			// ck_demos_erased：擦除行必须把内容列全部清空。
			title, desc = "", ""
		}
		var demoID int64
		err := pool.QueryRow(ctx, `
			INSERT INTO demos (slug, title, description, moderation_state, lifecycle_state, author_id, deleted_at, erased_at)
			VALUES ($1, NULLIF($2,''), NULLIF($3,''), 'approved', $4, $5,
			        CASE WHEN $6 THEN now() ELSE NULL END,
			        CASE WHEN $6 THEN now() ELSE NULL END)
			RETURNING id`,
			s.slug, title, desc, s.lifecycle, authorUserID, s.erased).Scan(&demoID)
		if err != nil {
			t.Fatalf("插入 %s 失败: %v", s.slug, err)
		}

		var revID int64
		if err := pool.QueryRow(ctx, `
			INSERT INTO demo_revisions (demo_id, rev_no, source, manifest_hash, file_count)
			VALUES ($1, 1, 'upload', repeat('a', 64), 1) RETURNING id`, demoID).Scan(&revID); err != nil {
			t.Fatalf("插入 %s 的版本失败: %v", s.slug, err)
		}
		if _, err := pool.Exec(ctx, `UPDATE demos SET current_revision_id = $1 WHERE id = $2`, revID, demoID); err != nil {
			t.Fatalf("设置 %s 的当前版本失败: %v", s.slug, err)
		}

		// 内容写两份：磁盘上的真实文件，和 storage 期望的 key。
		// 磁盘布局用 storage 的 key 规则 `demos/{slug}/r{revNo}/`，与 URL 里的 v{n} 无关。
		root := filepath.Join(dir, "demos", s.slug, "r1")
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatalf("建目录失败: %v", err)
		}
		files := map[string]string{
			"index.html": "<!doctype html><html><head><title>" + s.slug + "</title></head><body>hi</body></html>",
			"style.css":  "body{color:#000}",
			"sub/a.html": "<html><head></head><body>sub</body></html>",
		}
		for name, body := range files {
			p := filepath.Join(root, filepath.FromSlash(name))
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatalf("建子目录失败: %v", err)
			}
			if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
				t.Fatalf("写 %s 失败: %v", name, err)
			}
		}
	}
}

// mkAccount 一次装好 accounts / profiles / account_emails / users / web_sessions。
func mkAccount(t *testing.T, pool *pgxpool.Pool, sub, sessionID, username, role string) {
	t.Helper()
	ctx := context.Background()
	const sql = `
WITH a AS (
    INSERT INTO accounts (sub, status, role, level) VALUES ($1, 'active', $4, 1) RETURNING id
), p AS (
    INSERT INTO profiles (account_id, username, display_name) SELECT id, $3, $3 FROM a
), e AS (
    INSERT INTO account_emails (account_id, email, is_primary, verified_at)
    SELECT id, $3 || '@example.com', true, now() FROM a
), u AS (
    INSERT INTO users (sub) VALUES ($1)
)
INSERT INTO web_sessions (id, account_sub, username, display_name, role, status, verified_at)
SELECT $2, $1, $3, $3, $4, 'active', now() FROM a`
	if _, err := pool.Exec(ctx, sql, sub, sessionID, username, role); err != nil {
		t.Fatalf("装配账号 %s 失败: %v", username, err)
	}
}

func newPreview(t *testing.T, pool *pgxpool.Pool, dir string) *Preview {
	t.Helper()
	local, err := storage.NewLocal(dir)
	if err != nil {
		t.Fatalf("建本地存储失败: %v", err)
	}
	return NewPreview(store.New(pool), local, integrationIdentity(t, pool))
}

// previewGet 走真实路由：预览的 slug 与剩余路径由 chi 解析，
// 直接调 handler 会拿不到 URL 参数（chi.URLParam 依赖路由上下文）。
func previewGet(p *Preview, target, sessionID string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, target, nil)
	if sessionID != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sessionID})
	}
	w := httptest.NewRecorder()
	NewRouter(&stub{}, p).ServeHTTP(w, req)
	return w
}

func TestIntegrationPreviewVisibility(t *testing.T) {
	pool := integrationPool(t)
	dir := t.TempDir()
	seedPreviewWorld(t, pool, dir)
	p := newPreview(t, pool, dir)

	cases := []struct {
		name       string
		target     string
		session    string
		wantCode   int
		wantInBody string
	}{
		// 已发布：谁都能看，不需要会话。
		{"已发布-匿名", "/preview/pv-published/v1/index.html", "", 200, "pv-published"},
		{"已发布-当前版本别名", "/preview/pv-published/index.html", "", 200, "pv-published"},
		{"已发布-无尾斜杠", "/preview/pv-published", "", 200, "pv-published"},
		// 草稿：匿名看不到（404 而不是 403——不泄露「这个 slug 存在」）。
		{"草稿-匿名", "/preview/pv-draft/v1/index.html", "", 404, ""},
		// 草稿：作者本人能看。
		{"草稿-作者", "/preview/pv-draft/v1/index.html", pvAuthorSID, 200, "pv-draft"},
		// 草稿：持 demo.review 的审核员能看——这是审核的前提。
		{"草稿-审核员", "/preview/pv-draft/v1/index.html", pvModSID, 200, "pv-draft"},
		// 已归档：不是 published，因此走草稿同一条路径——作者与审核员能看，匿名不能。
		{"已归档-匿名", "/preview/pv-archived/v1/index.html", "", 404, ""},
		{"已归档-作者", "/preview/pv-archived/v1/index.html", pvAuthorSID, 200, "pv-archived"},
		// 已擦除：对所有人都不存在，连作者也不行。
		{"已擦除-作者", "/preview/pv-erased/v1/index.html", pvAuthorSID, 404, ""},
		{"已擦除-审核员", "/preview/pv-erased/v1/index.html", pvModSID, 404, ""},
		// 不存在的 slug。
		{"不存在", "/preview/pv-nope/v1/index.html", "", 404, ""},
		// 版本不存在 / 文件不存在。
		{"版本不存在", "/preview/pv-published/v9/index.html", "", 404, ""},
		{"文件不存在", "/preview/pv-published/v1/nope.html", "", 404, ""},
		// 目录穿越：storage 的 key 校验要挡住。
		{"穿越", "/preview/pv-published/v1/../../etc/passwd", "", 404, ""},
	}
	for _, c := range cases {
		w := previewGet(p, c.target, c.session)
		if w.Code != c.wantCode {
			t.Errorf("%s（%s）：期望 %d，实际 %d", c.name, c.target, c.wantCode, w.Code)
			continue
		}
		if c.wantInBody != "" && !strings.Contains(w.Body.String(), c.wantInBody) {
			t.Errorf("%s：响应体里没有 %q，实际 %q", c.name, c.wantInBody, w.Body.String())
		}
	}
}

func TestIntegrationPreviewHeaders(t *testing.T) {
	pool := integrationPool(t)
	dir := t.TempDir()
	seedPreviewWorld(t, pool, dir)
	p := newPreview(t, pool, dir)

	// 已发布 + 版本化：永久缓存 + CSP 沙箱 + 注入 base。
	w := previewGet(p, "/preview/pv-published/v1/index.html", "")
	if cc := w.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Errorf("已发布版本化应 immutable，实际 %q", cc)
	}
	if csp := w.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "sandbox") {
		t.Errorf("HTML 应带 CSP sandbox，实际 %q", csp)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type 应为 text/html，实际 %q", ct)
	}
	body := w.Body.String()
	if !strings.Contains(body, `<base href="/preview/pv-published/v1/">`) {
		t.Errorf("HTML 未注入 base：%q", body)
	}
	if nosniff := w.Header().Get("X-Content-Type-Options"); nosniff != "nosniff" {
		t.Errorf("缺 nosniff，实际 %q", nosniff)
	}

	// 已发布 + 当前版本别名：不能 immutable。
	w = previewGet(p, "/preview/pv-published/index.html", "")
	if cc := w.Header().Get("Cache-Control"); strings.Contains(cc, "immutable") {
		t.Errorf("当前版本别名不应 immutable，实际 %q", cc)
	}

	// 草稿：no-store，不能让中间层缓存。
	w = previewGet(p, "/preview/pv-draft/v1/index.html", pvAuthorSID)
	if cc := w.Header().Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Errorf("草稿应 no-store，实际 %q", cc)
	}

	// 非 HTML：不该注入 base，也不该带 CSP sandbox。
	w = previewGet(p, "/preview/pv-published/v1/style.css", "")
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/css") {
		t.Errorf("CSS 的 Content-Type 错：%q", ct)
	}
	if csp := w.Header().Get("Content-Security-Policy"); csp != "" {
		t.Errorf("非 HTML 不该带 CSP sandbox，实际 %q", csp)
	}
	if strings.Contains(w.Body.String(), "<base") {
		t.Errorf("CSS 被注入了 base：%q", w.Body.String())
	}

	// 子目录里的页面：base 必须指向版本根，否则 `../style.css` 会解析错。
	w = previewGet(p, "/preview/pv-published/v1/sub/a.html", "")
	if !strings.Contains(w.Body.String(), `<base href="/preview/pv-published/v1/">`) {
		t.Errorf("子目录页面的 base 不对：%q", w.Body.String())
	}
}
