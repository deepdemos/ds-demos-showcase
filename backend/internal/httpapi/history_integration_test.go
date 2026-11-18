package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/identity"
)

// 账号浏览历史（#426 第二半）：写入、去重、清空、关闭只停新增。
//
// 写入口是 POST /demos/{slug}/view，读/清空是 /me/history 三个端点。
// 这里直接调 handler，不起 HTTP 服务器——判据在「写没写、读不读得到、关掉之后还写不写」。

func historyReq(method, path, sid, body string) *http.Request {
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	if sid != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sid})
	}
	return req
}

func getHistory(t *testing.T, h *Handler, sid string) api.AccountHistory {
	t.Helper()
	rec := httptest.NewRecorder()
	h.GetMyHistory(rec, historyReq(http.MethodGet, "/api/v1/me/history", sid, ""), api.GetMyHistoryParams{})
	if rec.Code != http.StatusOK {
		t.Fatalf("读历史 = %d，想要 200（body=%s）", rec.Code, rec.Body.String())
	}
	var out api.AccountHistory
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("历史不是合法 JSON: %v（body=%s）", err, rec.Body.String())
	}
	return out
}

func viewAs(t *testing.T, h *Handler, sid, slug string) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.PostDemoView(rec, historyReq(http.MethodPost, "/api/v1/demos/"+slug+"/view", sid, ""), slug)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("记浏览 = %d，想要 204", rec.Code)
	}
}

func TestIntegrationAccountHistory(t *testing.T) {
	h, _, idSvc := newIdempotencyHandler(t)
	pool := integrationPool(t)

	author, sid := registerVerified(t, pool, idSvc, identity.RegisterInput{
		Email:       "it-history@example.com",
		Password:    "correct-horse-battery",
		Username:    "it-history-user",
		DisplayName: "历史用户",
	})
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM user_preferences WHERE account_id = (SELECT id FROM accounts WHERE sub = $1)`, author.Sub)
		cleanupAuthor(t, pool, author.Sub)
	})

	slugA := submitHTML(t, h, sid, "历史 A", "<h1>a</h1>")
	publishNow(t, pool, slugA)
	slugB := submitHTML(t, h, sid, "历史 B", "<h1>b</h1>")
	publishNow(t, pool, slugB)

	viewAs(t, h, sid, slugA)
	viewAs(t, h, sid, slugB)
	viewAs(t, h, sid, slugA) // 重复浏览：仍是两行，A 排到最前

	got := getHistory(t, h, sid)
	if !got.Enabled {
		t.Fatal("默认应该开启账号历史")
	}
	if got.Total != 2 || len(got.Items) != 2 {
		t.Fatalf("历史 = %d 条 / total %d，想要 2", len(got.Items), got.Total)
	}
	if got.Items[0].Slug != slugA || got.Items[1].Slug != slugB {
		t.Fatalf("顺序 = [%s, %s]，想要 [%s, %s]（最近的在前）",
			got.Items[0].Slug, got.Items[1].Slug, slugA, slugB)
	}

	// 关闭：只停新增，不删旧。
	rec := httptest.NewRecorder()
	h.UpdateMyHistorySettings(rec, historyReq(http.MethodPut, "/api/v1/me/history/settings", sid, `{"enabled":false}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("关闭历史 = %d，想要 200（body=%s）", rec.Code, rec.Body.String())
	}
	viewAs(t, h, sid, slugB)
	off := getHistory(t, h, sid)
	if off.Enabled {
		t.Fatal("关闭之后 enabled 应为 false")
	}
	if off.Total != 2 {
		t.Fatalf("关闭只该停新增，已有记录应保留 2 条，实际 %d", off.Total)
	}

	// 清空：幂等。
	for i := 0; i < 2; i++ {
		rec := httptest.NewRecorder()
		h.DeleteMyHistory(rec, historyReq(http.MethodDelete, "/api/v1/me/history", sid, ""))
		if rec.Code != http.StatusNoContent {
			t.Fatalf("第 %d 次清空 = %d，想要 204", i+1, rec.Code)
		}
	}
	if got := getHistory(t, h, sid); got.Total != 0 {
		t.Fatalf("清空之后 total = %d，想要 0", got.Total)
	}

	// 重新打开：又能记了。
	rec = httptest.NewRecorder()
	h.UpdateMyHistorySettings(rec, historyReq(http.MethodPut, "/api/v1/me/history/settings", sid, `{"enabled":true}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("重新打开 = %d，想要 200", rec.Code)
	}
	viewAs(t, h, sid, slugA)
	if got := getHistory(t, h, sid); got.Total != 1 {
		t.Fatalf("重新打开后 total = %d，想要 1", got.Total)
	}
}

// 未登录：三个端点都 401；登录但缺 enabled：400。
func TestIntegrationAccountHistoryRequiresLogin(t *testing.T) {
	h, _, _ := newIdempotencyHandler(t)

	calls := []struct {
		name string
		do   func(*httptest.ResponseRecorder)
	}{
		{"GET", func(rec *httptest.ResponseRecorder) {
			h.GetMyHistory(rec, historyReq(http.MethodGet, "/api/v1/me/history", "", ""), api.GetMyHistoryParams{})
		}},
		{"DELETE", func(rec *httptest.ResponseRecorder) {
			h.DeleteMyHistory(rec, historyReq(http.MethodDelete, "/api/v1/me/history", "", ""))
		}},
		{"PUT", func(rec *httptest.ResponseRecorder) {
			h.UpdateMyHistorySettings(rec, historyReq(http.MethodPut, "/api/v1/me/history/settings", "", `{"enabled":true}`))
		}},
	}
	for _, tc := range calls {
		rec := httptest.NewRecorder()
		tc.do(rec)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s 未登录 = %d，想要 401", tc.name, rec.Code)
		}
	}

	// 登录但请求体缺 enabled：400（不能把「没传」当成 false）。
	_, _, idSvc := newIdempotencyHandler(t)
	pool := integrationPool(t)
	acc, sid := registerVerified(t, pool, idSvc, identity.RegisterInput{
		Email:       "it-history-bad@example.com",
		Password:    "correct-horse-battery",
		Username:    "it-history-bad",
		DisplayName: "坏请求",
	})
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM user_preferences WHERE account_id = (SELECT id FROM accounts WHERE sub = $1)`, acc.Sub)
		cleanupAuthor(t, pool, acc.Sub)
	})
	rec := httptest.NewRecorder()
	h.UpdateMyHistorySettings(rec, historyReq(http.MethodPut, "/api/v1/me/history/settings", sid, `{}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("缺 enabled = %d，想要 400（body=%s）", rec.Code, rec.Body.String())
	}
}

// 上限：history.max_entries_account 调成 2 之后只留最近两条。
func TestIntegrationAccountHistoryCap(t *testing.T) {
	h, _, idSvc := newIdempotencyHandler(t)
	pool := integrationPool(t)

	author, sid := registerVerified(t, pool, idSvc, identity.RegisterInput{
		Email:       "it-history-cap@example.com",
		Password:    "correct-horse-battery",
		Username:    "it-history-cap",
		DisplayName: "上限用户",
	})
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM user_preferences WHERE account_id = (SELECT id FROM accounts WHERE sub = $1)`, author.Sub)
		cleanupAuthor(t, pool, author.Sub)
	})

	if _, err := pool.Exec(context.Background(), `
INSERT INTO site_settings (key, scope, value_type, value)
VALUES ('history.max_entries_account', 'site', 'number', '2'::jsonb)
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`); err != nil {
		t.Fatalf("写下限失败: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM site_settings WHERE key = 'history.max_entries_account'`)
	})

	for i, title := range []string{"上限 A", "上限 B", "上限 C"} {
		slug := submitHTML(t, h, sid, title, "<h1>cap</h1>")
		publishNow(t, pool, slug)
		viewAs(t, h, sid, slug)
		_ = i
	}

	got := getHistory(t, h, sid)
	if got.Total != 2 {
		t.Fatalf("上限 2 时 total = %d，想要 2", got.Total)
	}
}

// 未公开作品不能被别人借「浏览历史」读到标题/封面（PR #475 复核 F1）。
func TestIntegrationAccountHistoryHidesUnpublished(t *testing.T) {
	h, _, idSvc := newIdempotencyHandler(t)
	pool := integrationPool(t)

	author, sid := registerVerified(t, pool, idSvc, identity.RegisterInput{
		Email: "it-history-a@example.com", Password: "correct-horse-battery",
		Username: "it-history-a", DisplayName: "作者",
	})
	t.Cleanup(func() { cleanupAuthor(t, pool, author.Sub) })
	other, otherSID := registerVerified(t, pool, idSvc, identity.RegisterInput{
		Email: "it-history-b@example.com", Password: "correct-horse-battery",
		Username: "it-history-b", DisplayName: "路人",
	})
	t.Cleanup(func() { cleanupAuthor(t, pool, other.Sub) })

	draft := submitHTML(t, h, sid, "未公开历史", "<h1>draft</h1>")
	public := submitHTML(t, h, sid, "公开历史", "<h1>public</h1>")
	publishNow(t, pool, public)

	viewAs(t, h, otherSID, draft)
	if got := getHistory(t, h, otherSID); got.Total != 0 {
		t.Fatalf("未公开作品进了别人的历史：%+v", got.Items)
	}

	viewAs(t, h, otherSID, public)
	if got := getHistory(t, h, otherSID); got.Total != 1 || got.Items[0].Slug != public {
		t.Fatalf("公开作品历史 = %+v，想要 1 条 %s", got.Items, public)
	}

	// unlisted 也能通过链接打开，应当进历史。
	unlisted := submitHTML(t, h, sid, "unlisted 历史", "<h1>u</h1>")
	publishNow(t, pool, unlisted)
	if _, err := pool.Exec(context.Background(), `
UPDATE demo_channels SET visibility = 'unlisted'
WHERE demo_id = (SELECT id FROM demos WHERE slug = $1)`, unlisted); err != nil {
		t.Fatalf("改 unlisted 失败: %v", err)
	}
	viewAs(t, h, otherSID, unlisted)

	// archived 是墓碑，不展示内容，也不该进历史。
	archived := submitHTML(t, h, sid, "archived 历史", "<h1>a</h1>")
	publishNow(t, pool, archived)
	if _, err := pool.Exec(context.Background(), `UPDATE demos SET lifecycle_state='archived' WHERE slug=$1`, archived); err != nil {
		t.Fatalf("置 archived 失败: %v", err)
	}
	viewAs(t, h, otherSID, archived)

	got := getHistory(t, h, otherSID)
	if got.Total != 2 {
		t.Fatalf("历史 total = %d，想要 2（公开 + unlisted，archived 不计）：%+v", got.Total, got.Items)
	}
	seen := map[string]bool{}
	for _, item := range got.Items {
		seen[item.Slug] = true
	}
	if !seen[public] || !seen[unlisted] || seen[archived] {
		t.Fatalf("历史条目 = %+v，想要 public+unlisted、不含 archived", seen)
	}
}

// 作者判据必须走 users.id：把 users.id 挪开之后，作者本人仍能看到自己的草稿。
// 旧实现用 accounts.id 比较，在这条用例下会空历史。
func TestIntegrationAccountHistoryAuthorIDDrift(t *testing.T) {
	h, _, idSvc := newIdempotencyHandler(t)
	pool := integrationPool(t)

	author, sid := registerVerified(t, pool, idSvc, identity.RegisterInput{
		Email: "it-history-drift@example.com", Password: "correct-horse-battery",
		Username: "it-history-drift", DisplayName: "漂移作者",
	})
	t.Cleanup(func() { cleanupAuthor(t, pool, author.Sub) })

	// 人为制造 accounts.id != users.id：删掉注册时那一行，再用 OVERRIDING SYSTEM VALUE
	// 插一个远离当前序列的 users.id；投稿时 demos.author_id 会写这个新值。
	tx, err := pool.Begin(context.Background())
	if err != nil {
		t.Fatalf("开事务失败: %v", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err := tx.Exec(context.Background(), `DELETE FROM users WHERE sub = $1`, author.Sub); err != nil {
		t.Fatalf("删旧 users 行失败: %v", err)
	}
	if _, err := tx.Exec(context.Background(), `
INSERT INTO users (id, sub) OVERRIDING SYSTEM VALUE VALUES (900000000, $1)`, author.Sub); err != nil {
		t.Fatalf("插漂移 users 行失败: %v", err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatalf("提交漂移失败: %v", err)
	}

	draft := submitHTML(t, h, sid, "漂移作者的草稿", "<h1>drift</h1>")
	viewAs(t, h, sid, draft)

	got := getHistory(t, h, sid)
	if got.Total != 1 || got.Items[0].Slug != draft {
		t.Fatalf("作者漂移后历史 = %+v，想要 1 条 %s", got.Items, draft)
	}
}
