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
		// visibility 为空表示不建渠道行。
		//
		// 触发器 trg_demos_guard 拒绝给未审过的作品上架渠道，所以
		// 「pending/rejected + 渠道」这个组合在库里造不出来——
		// pv-draft 因此没有渠道行，它的判定由 lifecycle 兜住，不靠渠道。
		visibility string
		// hasPublished：这行是否**有过**对外版本（迁移 0014 的 published_revision_id）。
		// 与 visibility 是两件事：hidden 的作品也曾经对外过（它只是不上列表、不给直链），
		// 而 draft 从没对外过。写成显式字段而不是由 lifecycle 推，是为了让每个用例
		// 自己说清「我这条数据代表什么状态」。
		hasPublished bool
	}
	specs := []demoSpec{
		{"pv-draft", "draft", false, "", false},
		{"pv-published", "published", false, "listed", true},
		// hidden 渠道：作品对外不存在。预览必须与下载同一口径地拒掉它。
		{"pv-hidden", "published", false, "hidden", true},
		{"pv-archived", "archived", false, "listed", true},
		{"pv-erased", "published", true, "listed", true},
		// 复审中：审核态被退回 pending，但对外内容仍是上一版、照常在服务。
		// 这是版本分离之后新增的组合，也最容易被人按审核态写成 404。
		{"pv-under-review", "published", false, "listed", true},
		// 作者已经交了一版未过审的新版：对外版本仍是 r1，当前版本是 r2。
		// 这条钉的是「路径里的版本号不能绕过审核」——作者改稿期间
		// 匿名访客不许靠猜 `/v2/` 看到还没审的内容。
		{"pv-unapproved-new", "published", false, "listed", true},
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
			VALUES ($1, NULLIF($2,''), NULLIF($3,''),
			        -- 复审中的那条：审核态 pending 但对外内容仍在（版本分离的正常态）。
			        CASE WHEN $1 = 'pv-under-review' THEN 'pending' ELSE 'approved' END,
			        $4, $5,
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
		// 已发布的作品要有一个对外版本指针（迁移 0014）：没它渠道上不去，
		// 详情/预览/下载的公开读路径也拿不到内容。
		// 草稿与「发布但没审过」的作品按真实情况留空——它们从没对外过。
		if _, err := pool.Exec(ctx, `
			UPDATE demos
			SET current_revision_id = $1::bigint,
			    published_revision_id = CASE WHEN $3 THEN $1::bigint ELSE NULL END
			WHERE id = $2`, revID, demoID, s.hasPublished); err != nil {
			t.Fatalf("设置 %s 的当前版本失败: %v", s.slug, err)
		}

		// 渠道行必须**排在指针之后**：触发器要求 listed 渠道对应的作品已经有对外版本
		// （迁移 0014）。先插渠道会红在「作品 N 还没有对外版本」——这条顺序本身就是
		// 不变量的一部分，不是种子的偶然写法。
		if s.visibility != "" {
			if _, err := pool.Exec(ctx, `
				INSERT INTO demo_channels (demo_id, channel, visibility, lang)
				VALUES ($1, 'deep', $2, 'zh')`, demoID, s.visibility); err != nil {
				t.Fatalf("插入 %s 的渠道失败: %v", s.slug, err)
			}
		}

		// 给「作者交了一版未过审的新版」那条造第二个版本：当前版本指过去，
		// 对外版本留在 r1。
		if s.slug == "pv-unapproved-new" {
			var rev2 int64
			if err := pool.QueryRow(ctx, `
				INSERT INTO demo_revisions (demo_id, rev_no, source, manifest_hash, file_count)
				VALUES ($1, 2, 'upload', repeat('b', 64), 1) RETURNING id`, demoID).Scan(&rev2); err != nil {
				t.Fatalf("插入 %s 的 r2 失败: %v", s.slug, err)
			}
			if _, err := pool.Exec(ctx,
				`UPDATE demos SET current_revision_id = $1 WHERE id = $2`, rev2, demoID); err != nil {
				t.Fatalf("设置 %s 的 r2 失败: %v", s.slug, err)
			}
			root2 := filepath.Join(dir, "demos", s.slug, "r2")
			if err := os.MkdirAll(root2, 0o755); err != nil {
				t.Fatalf("建 %s 的 r2 目录失败: %v", s.slug, err)
			}
			if err := os.WriteFile(filepath.Join(root2, "index.html"),
				[]byte("<!doctype html><html><head><title>unapproved-r2</title></head><body>未过审</body></html>"),
				0o644); err != nil {
				t.Fatalf("写 %s 的 r2 内容失败: %v", s.slug, err)
			}
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
			// 顶层文档会执行脚本的类型。没有 CSP sandbox 时它们能读主站同源数据。
			"evil.svg":   `<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`,
			"evil.xhtml": `<html xmlns="http://www.w3.org/1999/xhtml"><script>alert(1)</script></html>`,
			"evil.xml":   `<?xml version="1.0"?><root><script>alert(1)</script></root>`,
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

	// 单文件 SVG 作品：`entry_path = 'index.svg'`，磁盘上只有 index.svg。
	// 预览曾经把入口硬编码成 index.html，所以它打开就是 404
	//（真实事故：用户上传一张 SVG，投稿成功但预览页打不开）。
	seedSingleFileSVG(t, pool, dir, authorUserID)
}

// seedSingleFileSVG 造一条「单文件 SVG」草稿作品：入口是 index.svg，不是 index.html。
//
// 这条数据必须单独造而不能并进上面的循环：循环里每个作品的入口都是 index.html，
// 而这里要钉的恰恰是「入口不叫 index.html 时预览仍要打开」。
func seedSingleFileSVG(t *testing.T, pool *pgxpool.Pool, dir string, authorUserID int64) {
	t.Helper()
	ctx := context.Background()

	var demoID, revID int64
	if err := pool.QueryRow(ctx, `
INSERT INTO demos (slug, title, description, moderation_state, lifecycle_state, author_id)
VALUES ('pv-svg', '标题 pv-svg', '简介', 'pending', 'draft', $1)
RETURNING id`, authorUserID).Scan(&demoID); err != nil {
		t.Fatalf("插入 pv-svg 失败: %v", err)
	}
	if err := pool.QueryRow(ctx, `
INSERT INTO demo_revisions (demo_id, rev_no, source, manifest_hash, file_count, entry_path, single_file)
VALUES ($1, 1, 'upload', repeat('c', 64), 1, 'index.svg', 'svg')
RETURNING id`, demoID).Scan(&revID); err != nil {
		t.Fatalf("插入 pv-svg 的版本失败: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`UPDATE demos SET current_revision_id = $1 WHERE id = $2`, revID, demoID); err != nil {
		t.Fatalf("设置 pv-svg 的当前版本失败: %v", err)
	}

	root := filepath.Join(dir, "demos", "pv-svg", "r1")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("建 pv-svg 目录失败: %v", err)
	}
	svg := `<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"><rect width="10" height="10"/></svg>`
	if err := os.WriteFile(filepath.Join(root, "index.svg"), []byte(svg), 0o644); err != nil {
		t.Fatalf("写 pv-svg 的 index.svg 失败: %v", err)
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
	NewRouter(&stub{}, p, nil).ServeHTTP(w, req)
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
		// 复审中：审核态 pending，但对外版本仍在 → 匿名照常看得到。
		// 这条钉的是「审核不影响已发布内容」——迁移 0014 之前它是 404。
		{"复审中-匿名仍可见", "/preview/pv-under-review/v1/index.html", "", 200, "pv-under-review"},
		// 未过审的新版本：路径里写 `/v2/` 也不行。这条是「作者改稿期间匿名不许看新版」。
		{"未过审新版-匿名", "/preview/pv-unapproved-new/v2/index.html", "", 404, ""},
		// 但作者自己要能看——他的「预览新版」按钮点下去就是这个地址。
		{"未过审新版-作者", "/preview/pv-unapproved-new/v2/index.html", pvAuthorSID, 200, "unapproved-r2"},
		// 对外版本仍然匿名可看，且不带版本号时默认取对外版本。
		{"对外版本-匿名", "/preview/pv-unapproved-new/v1/index.html", "", 200, "pv-unapproved-new"},
		{"对外版本-默认", "/preview/pv-unapproved-new/index.html", "", 200, "pv-unapproved-new"},
		// 草稿：匿名看不到（404 而不是 403——不泄露「这个 slug 存在」）。
		{"草稿-匿名", "/preview/pv-draft/v1/index.html", "", 404, ""},
		// 草稿：作者本人能看。
		{"草稿-作者", "/preview/pv-draft/v1/index.html", pvAuthorSID, 200, "pv-draft"},
		// 草稿：持 demo.review 的审核员能看——这是审核的前提。
		{"草稿-审核员", "/preview/pv-draft/v1/index.html", pvModSID, 200, "pv-draft"},
		// hidden 渠道：已发布 + 已审过，但渠道对外不可见——匿名 404。
		// 这条是本轮补的判定；之前预览完全不看渠道，hidden 作品的直链能看完整内容。
		{"hidden渠道-匿名", "/preview/pv-hidden/v1/index.html", "", 404, ""},
		// 但作者与审核员仍然要看得到：他们看的是「还没对外」的东西，本来就该绕过渠道判定。
		{"hidden渠道-作者", "/preview/pv-hidden/v1/index.html", pvAuthorSID, 200, "pv-hidden"},
		{"hidden渠道-审核员", "/preview/pv-hidden/v1/index.html", pvModSID, 200, "pv-hidden"},
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

// 单文件 SVG：不带文件名打开预览时，入口要按 `entry_path` 取（index.svg），
// 而不是硬编码的 index.html。这条用例就是「上传一张 SVG，预览页 404」那次事故。
func TestIntegrationPreviewSingleFileEntry(t *testing.T) {
	pool := integrationPool(t)
	dir := t.TempDir()
	seedPreviewWorld(t, pool, dir)
	p := newPreview(t, pool, dir)

	// 作者看自己的草稿：不带文件名，靠 entry_path 兜底。
	w := previewGet(p, "/preview/pv-svg/", pvAuthorSID)
	if w.Code != http.StatusOK {
		t.Fatalf("单文件 SVG 的预览 = %d，想要 200；body=%q", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "image/svg+xml") {
		t.Fatalf("Content-Type = %q，想要 image/svg+xml", ct)
	}
	if !strings.Contains(w.Body.String(), "<svg") {
		t.Fatalf("响应体不是这张 SVG：%q", w.Body.String())
	}

	// 带版本号的写法同样要能打开。
	if w := previewGet(p, "/preview/pv-svg/v1/", pvAuthorSID); w.Code != http.StatusOK {
		t.Fatalf("/preview/pv-svg/v1/ = %d，想要 200", w.Code)
	}

	// 匿名仍然 404：入口修好了不等于草稿对谁都能看。
	if w := previewGet(p, "/preview/pv-svg/", ""); w.Code != http.StatusNotFound {
		t.Fatalf("匿名看草稿 = %d，想要 404", w.Code)
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

	// **可执行脚本的文档类型必须带 CSP sandbox**，不只是 text/html。
	// 这条测试曾经写成「非 HTML 一律不加 CSP」，正好把
	// 「SVG 预览可以执行脚本」锁死在测试里。
	for _, name := range []string{"evil.svg", "evil.xhtml", "evil.xml"} {
		w = previewGet(p, "/preview/pv-published/v1/"+name, "")
		csp := w.Header().Get("Content-Security-Policy")
		if !strings.Contains(csp, "sandbox") {
			t.Errorf("%s 是可执行脚本的文档类型，必须带 CSP sandbox，实际 %q", name, csp)
		}
	}

	// 纯资源（CSS）：不该注入 base，也不该带 CSP sandbox。
	w = previewGet(p, "/preview/pv-published/v1/style.css", "")
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/css") {
		t.Errorf("CSS 的 Content-Type 错：%q", ct)
	}
	if csp := w.Header().Get("Content-Security-Policy"); csp != "" {
		t.Errorf("CSS 是纯资源，不该带 CSP sandbox，实际 %q", csp)
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
