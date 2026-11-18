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
