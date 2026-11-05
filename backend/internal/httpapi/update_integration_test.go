package httpapi

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/demo"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/identity"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/platform/storage"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// 更新端点走真实路由：契约里是 PUT /api/v1/demos/{slug}，路径参数由 chi 解析，
// 直接调处理器拿不到 slug。
func TestIntegrationUpdateDemo(t *testing.T) {
	pool := integrationPool(t)
	q := store.New(pool)
	idSvc := integrationIdentity(t, pool)
	files, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("初始化存储失败: %v", err)
	}
	demoSvc := demo.New(pool, q, files, storage.DefaultLimits(), 200<<20, 0, nil)
	h := New(pool, q, idSvc, nil, demoSvc)

	acc, sid := registerVerified(t, pool, idSvc, identity.RegisterInput{
		Email:       "it-update@example.com",
		Password:    "updatetest123",
		Username:    "it-update",
		DisplayName: "更新作者",
	})
	t.Cleanup(func() { cleanupAuthor(t, pool, acc.Sub) })

	// 1) 投稿（单文件 html 最简单）。
	slug := submitHTML(t, h, sid, "更新用作品", `<!doctype html><p>v1</p>`)
	if slug == "" {
		t.Fatalf("投稿没有返回 slug")
	}

	// 2) 作者能看到自己的待审稿件（不然「更新」页连当前内容都读不出来）。
	detail := getDemoAs(t, h, slug, sid)
	if detail.Code != http.StatusOK {
		t.Fatalf("作者看自己的 pending 稿件 = %d，想要 200；响应体：%s", detail.Code, detail.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(detail.Body.Bytes(), &got); err != nil {
		t.Fatalf("解析详情失败: %v", err)
	}
	if got["canEdit"] != true {
		t.Fatalf("作者看自己的作品 canEdit 应为 true，实际 %v", got["canEdit"])
	}

	// 访客看同一件作品必须 404：作者视图不能顺带把草稿公开出去。
	if guest := getDemoAs(t, h, slug, ""); guest.Code != http.StatusNotFound {
		t.Fatalf("访客看待审稿件 = %d，想要 404", guest.Code)
	}

	// 3) 只改文字：200，版本号 2，审核态回到 pending。
	rec := updateDemo(t, h, sid, slug, "更新后标题", "更新后简介", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("更新 = %d，想要 200；响应体：%s", rec.Code, rec.Body.String())
	}
	var res map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("解析更新回执失败: %v", err)
	}
	if res["revisionNo"] != float64(2) {
		t.Fatalf("revisionNo = %v，想要 2", res["revisionNo"])
	}
	if res["moderationState"] != "pending" {
		t.Fatalf("moderationState = %v，想要 pending", res["moderationState"])
	}

	// 4) 作者视图里换成新标题。
	detail = getDemoAs(t, h, slug, sid)
	if err := json.Unmarshal(detail.Body.Bytes(), &got); err != nil {
		t.Fatalf("解析详情失败: %v", err)
	}
	if got["title"] != "更新后标题" {
		t.Fatalf("作者视图标题 = %v，想要「更新后标题」", got["title"])
	}

	// 5) 不带 package 却改 demoType → 400。
	rec = updateDemoWithType(t, h, sid, slug, "更新后标题", "zip")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("只改类型 = %d，想要 400；响应体：%s", rec.Code, rec.Body.String())
	}

	// 6) 未登录 → 401。
	if rec := updateDemo(t, h, "", slug, "x", "", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("未登录更新 = %d，想要 401", rec.Code)
	}

	// 7) 别人（不存在）的作品 → 404。
	if rec := updateDemo(t, h, sid, "nobody-has-this-slug", "x", "", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("更新不存在的作品 = %d，想要 404", rec.Code)
	}
}

// TestIntegrationUpdateDemoRouteUsesPUT 钉住路由形状：契约是 PUT，
// 写成 POST 会让生成的客户端调不到，而两边各自单测都过得去。
func TestIntegrationUpdateDemoRouteUsesPUT(t *testing.T) {
	router := NewRouter(&stub{}, nil, nil)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/demos/demo-slug", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	// 未接线的 Handler 返回 503，但**不能是 404/405**——那说明路由没挂上。
	if rec.Code == http.StatusNotFound || rec.Code == http.StatusMethodNotAllowed {
		t.Fatalf("PUT /api/v1/demos/{slug} 路由没挂上，状态码 %d", rec.Code)
	}
}

// ---- 辅助 ----

func submitHTML(t *testing.T, h *Handler, sid, title, body string) string {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := mw.WriteField("title", title); err != nil {
		t.Fatal(err)
	}
	if err := mw.WriteField("description", "简介"); err != nil {
		t.Fatal(err)
	}
	if err := mw.WriteField("demoType", "web"); err != nil {
		t.Fatal(err)
	}
	part, err := mw.CreateFormFile("package", "index.html")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/demos", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sid})
	rec := httptest.NewRecorder()
	h.SubmitDemo(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("投稿 = %d，想要 201；响应体：%s", rec.Code, rec.Body.String())
	}
	var res map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("解析投稿回执失败: %v", err)
	}
	slug, _ := res["slug"].(string)
	return slug
}

func updateDemo(t *testing.T, h *Handler, sid, slug, title, description string, pkg []byte) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := mw.WriteField("title", title); err != nil {
		t.Fatal(err)
	}
	if description != "" {
		if err := mw.WriteField("description", description); err != nil {
			t.Fatal(err)
		}
	}
	if pkg != nil {
		part, err := mw.CreateFormFile("package", "index.html")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(pkg); err != nil {
			t.Fatal(err)
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPut, "/api/v1/demos/"+slug, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if sid != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sid})
	}
	rec := httptest.NewRecorder()
	NewRouter(h, nil, nil).ServeHTTP(rec, req)
	return rec
}

func updateDemoWithType(t *testing.T, h *Handler, sid, slug, title, demoType string) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := mw.WriteField("title", title); err != nil {
		t.Fatal(err)
	}
	if err := mw.WriteField("demoType", demoType); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPut, "/api/v1/demos/"+slug, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sid})
	rec := httptest.NewRecorder()
	NewRouter(h, nil, nil).ServeHTTP(rec, req)
	return rec
}

func getDemoAs(t *testing.T, h *Handler, slug, sid string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/demos/"+slug, nil)
	if sid != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sid})
	}
	rec := httptest.NewRecorder()
	NewRouter(h, nil, nil).ServeHTTP(rec, req)
	return rec
}
