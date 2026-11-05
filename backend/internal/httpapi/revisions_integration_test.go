package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/demo"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/identity"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/platform/storage"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// 版本列表与回滚的传输层测试：走真实路由（`{slug}` 与 `{revNo}` 由 chi 解析）。

func newRevisionTestHandler(t *testing.T) (*Handler, *identity.Service) {
	t.Helper()
	pool := integrationPool(t)
	q := store.New(pool)
	idSvc := integrationIdentity(t, pool)
	files, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("初始化存储失败: %v", err)
	}
	demoSvc := demo.New(pool, q, files, storage.DefaultLimits(), 200<<20, 0, nil)
	return New(pool, q, idSvc, nil, demoSvc), idSvc
}

func TestIntegrationListRevisionsHTTP(t *testing.T) {
	h, idSvc := newRevisionTestHandler(t)
	pool := integrationPool(t)

	acc, sid := registerVerified(t, pool, idSvc, identity.RegisterInput{
		Email:       "it-rev-list@example.com",
		Password:    "revlist12345",
		Username:    "it-rev-list",
		DisplayName: "版本作者",
	})
	t.Cleanup(func() { cleanupAuthor(t, pool, acc.Sub) })

	slug := submitHTML(t, h, sid, "版本列表 HTTP", `<!doctype html><p>v1</p>`)

	// 作者能看：一行，且是当前版本。
	rec := getRevisions(t, h, slug, sid)
	if rec.Code != http.StatusOK {
		t.Fatalf("作者看版本列表 = %d，想要 200；响应体：%s", rec.Code, rec.Body.String())
	}
	var list struct {
		Items []struct {
			RevNo     int  `json:"revNo"`
			IsCurrent bool `json:"isCurrent"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("解析版本列表失败: %v", err)
	}
	if len(list.Items) != 1 || list.Items[0].RevNo != 1 || !list.Items[0].IsCurrent {
		t.Fatalf("版本列表不对: %+v", list.Items)
	}

	// 未登录：401（不是 404——「你还没说你是谁」和「这件作品你没有」是两件事）。
	if rec := getRevisions(t, h, slug, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("未登录看版本列表 = %d，想要 401", rec.Code)
	}

	// 别人：404，不承认 slug 存在。
	_, otherSid := registerVerified(t, pool, idSvc, identity.RegisterInput{
		Email:       "it-rev-other@example.com",
		Password:    "revother1234",
		Username:    "it-rev-other",
		DisplayName: "路人",
	})
	var otherSub string
	if err := pool.QueryRow(t.Context(),
		`SELECT sub FROM accounts a JOIN account_emails e ON e.account_id = a.id WHERE e.email = $1`,
		"it-rev-other@example.com").Scan(&otherSub); err != nil {
		t.Fatalf("取另一个账号失败: %v", err)
	}
	t.Cleanup(func() { cleanupAuthor(t, pool, otherSub) })
	if rec := getRevisions(t, h, slug, otherSid); rec.Code != http.StatusNotFound {
		t.Fatalf("别人看版本列表 = %d，想要 404", rec.Code)
	}

	// 审核员能看别人的：这条是「审核要看历史」的直接需求。
	mkAccount(t, pool, "ITREVMOD", "it-rev-mod", "it-rev-mod", "moderator")
	if rec := getRevisions(t, h, slug, "it-rev-mod"); rec.Code != http.StatusOK {
		t.Fatalf("审核员看版本列表 = %d，想要 200；响应体：%s", rec.Code, rec.Body.String())
	}
}

func TestIntegrationRestoreRevisionHTTP(t *testing.T) {
	h, idSvc := newRevisionTestHandler(t)
	pool := integrationPool(t)

	acc, sid := registerVerified(t, pool, idSvc, identity.RegisterInput{
		Email:       "it-rev-restore@example.com",
		Password:    "revrestore12",
		Username:    "it-rev-restore",
		DisplayName: "回滚作者",
	})
	t.Cleanup(func() { cleanupAuthor(t, pool, acc.Sub) })

	slug := submitHTML(t, h, sid, "回滚 HTTP", `<!doctype html><p>v1</p>`)

	// 先改一版，才有东西可回滚（回滚到当前版本会被 400 挡住）。
	if rec := updateDemo(t, h, sid, slug, "回滚 HTTP v2", "", nil); rec.Code != http.StatusOK {
		t.Fatalf("更新 = %d，想要 200", rec.Code)
	}

	// 回滚到 r1 → 200，新版本号 3。
	rec := restoreRevision(t, h, sid, slug, "1")
	if rec.Code != http.StatusOK {
		t.Fatalf("回滚 = %d，想要 200；响应体：%s", rec.Code, rec.Body.String())
	}
	var res struct {
		Slug              string `json:"slug"`
		RestoredFromRevNo int    `json:"restoredFromRevNo"`
		RevisionNo        int    `json:"revisionNo"`
		ModerationState   string `json:"moderationState"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("解析回执失败: %v", err)
	}
	if res.RevisionNo != 3 || res.RestoredFromRevNo != 1 || res.ModerationState != "pending" {
		t.Fatalf("回执不对: %+v", res)
	}

	// 版本列表现在有 3 行，第一行是新的。
	if rec := getRevisions(t, h, slug, sid); rec.Code == http.StatusOK {
		var list struct {
			Items []struct {
				RevNo     int    `json:"revNo"`
				Source    string `json:"source"`
				IsCurrent bool   `json:"isCurrent"`
			} `json:"items"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
			t.Fatalf("解析版本列表失败: %v", err)
		}
		if len(list.Items) != 3 || list.Items[0].RevNo != 3 || !list.Items[0].IsCurrent {
			t.Fatalf("回滚后的版本列表不对: %+v", list.Items)
		}
		if list.Items[0].Source != "rollback" {
			t.Fatalf("新版本 source = %q，想要 rollback", list.Items[0].Source)
		}
	}

	// 未登录 401、版本号不是数字 400、不存在的版本 404。
	if rec := restoreRevision(t, h, "", slug, "1"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("未登录回滚 = %d，想要 401", rec.Code)
	}
	if rec := restoreRevision(t, h, sid, slug, "abc"); rec.Code != http.StatusBadRequest {
		t.Fatalf("版本号 abc = %d，想要 400", rec.Code)
	}
	if rec := restoreRevision(t, h, sid, slug, "99"); rec.Code != http.StatusNotFound {
		t.Fatalf("不存在的版本 = %d，想要 404", rec.Code)
	}
}

// TestIntegrationRestoreRouteUsesPOST 钉住方法：契约是 POST。
func TestIntegrationRestoreRouteUsesPOST(t *testing.T) {
	router := NewRouter(&stub{}, nil, nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/demos/some-slug/revisions/1/restore", nil))
	if rec.Code != http.StatusNotFound && rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET 回滚路径应当 404/405，实际 %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/demos/some-slug/revisions/1/restore", nil))
	if rec.Code == http.StatusNotFound || rec.Code == http.StatusMethodNotAllowed {
		t.Fatalf("POST 回滚路径没挂上，状态码 %d", rec.Code)
	}
}

// ---- 辅助 ----

func getRevisions(t *testing.T, h *Handler, slug, sid string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/demos/"+slug+"/revisions", nil)
	if sid != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sid})
	}
	rec := httptest.NewRecorder()
	NewRouter(h, nil, nil).ServeHTTP(rec, req)
	return rec
}

func restoreRevision(t *testing.T, h *Handler, sid, slug, revNo string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/demos/"+slug+"/revisions/"+revNo+"/restore", nil)
	if sid != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sid})
	}
	rec := httptest.NewRecorder()
	NewRouter(h, nil, nil).ServeHTTP(rec, req)
	return rec
}
