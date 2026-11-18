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

// 作品申诉（#463）：作者能提交一次、看到处理中；非作者 404；在途申诉挡写操作。

func appealReq(method, path, sid, body string) *http.Request {
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

func TestIntegrationDemoAppeal(t *testing.T) {
	h, _, idSvc := newIdempotencyHandler(t)
	pool := integrationPool(t)

	author, sid := registerVerified(t, pool, idSvc, identity.RegisterInput{
		Email:       "it-appeal@example.com",
		Password:    "correct-horse-battery",
		Username:    "it-appeal-author",
		DisplayName: "申诉作者",
	})
	t.Cleanup(func() { cleanupAuthor(t, pool, author.Sub) })

	slug := submitHTML(t, h, sid, "被移除的作品", "<h1>x</h1>")
	publishNow(t, pool, slug)
	if _, err := pool.Exec(context.Background(), `
UPDATE demos SET lifecycle_state='removed', moderation_state='approved', updated_at=GREATEST(now(), created_at)
WHERE slug=$1`, slug); err != nil {
		t.Fatalf("置为 removed 失败: %v", err)
	}

	// 初始：能提交，还没有申诉。
	rec := httptest.NewRecorder()
	h.GetDemoAppeal(rec, appealReq(http.MethodGet, "/api/v1/demos/"+slug+"/appeal", sid, ""), slug)
	if rec.Code != http.StatusOK {
		t.Fatalf("读申诉 = %d，想要 200（body=%s）", rec.Code, rec.Body.String())
	}
	var state api.DemoAppealState
	if err := json.Unmarshal(rec.Body.Bytes(), &state); err != nil {
		t.Fatalf("申诉状态不是合法 JSON: %v", err)
	}
	if !state.CanSubmit || state.Appeal != nil {
		t.Fatalf("初始状态 = %+v", state)
	}

	// 提交一次。
	rec = httptest.NewRecorder()
	h.SubmitDemoAppeal(rec, appealReq(http.MethodPost, "/api/v1/demos/"+slug+"/appeal", sid,
		`{"reason":"这是一次误判，请复核"}`), slug)
	if rec.Code != http.StatusCreated {
		t.Fatalf("提交申诉 = %d，想要 201（body=%s）", rec.Code, rec.Body.String())
	}
	var created api.DemoAppeal
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("申诉不是合法 JSON: %v", err)
	}
	if created.Status != api.DemoAppealStatusPending || created.Reason == "" {
		t.Fatalf("申诉 = %+v", created)
	}

	// 处理中：不能再提交，也不能改作品。
	rec = httptest.NewRecorder()
	h.GetDemoAppeal(rec, appealReq(http.MethodGet, "/api/v1/demos/"+slug+"/appeal", sid, ""), slug)
	if err := json.Unmarshal(rec.Body.Bytes(), &state); err != nil {
		t.Fatalf("申诉状态不是合法 JSON: %v", err)
	}
	if state.CanSubmit || state.Appeal == nil || state.Appeal.Status != api.DemoAppealStatusPending {
		t.Fatalf("处理中状态 = %+v", state)
	}

	rec = httptest.NewRecorder()
	h.SubmitDemoAppeal(rec, appealReq(http.MethodPost, "/api/v1/demos/"+slug+"/appeal", sid,
		`{"reason":"再提一次"}`), slug)
	if rec.Code != http.StatusConflict {
		t.Fatalf("重复提交 = %d，想要 409", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.UpdateDemo(rec, appealReq(http.MethodPost, "/api/v1/demos/"+slug, sid, ""), slug)
	if rec.Code != http.StatusConflict {
		t.Fatalf("在途申诉时改作品 = %d，想要 409（body=%s）", rec.Code, rec.Body.String())
	}
	var errBody api.Error
	if err := json.Unmarshal(rec.Body.Bytes(), &errBody); err != nil || errBody.Code != api.AppealPending {
		t.Fatalf("错误码 = %+v err=%v，想要 appeal_pending", errBody, err)
	}
}

func TestIntegrationDemoAppealAuthorization(t *testing.T) {
	h, _, idSvc := newIdempotencyHandler(t)
	pool := integrationPool(t)

	author, sid := registerVerified(t, pool, idSvc, identity.RegisterInput{
		Email:       "it-appeal-own@example.com",
		Password:    "correct-horse-battery",
		Username:    "it-appeal-own",
		DisplayName: "作者",
	})
	t.Cleanup(func() { cleanupAuthor(t, pool, author.Sub) })
	other, otherSID := registerVerified(t, pool, idSvc, identity.RegisterInput{
		Email:       "it-appeal-other@example.com",
		Password:    "correct-horse-battery",
		Username:    "it-appeal-other",
		DisplayName: "路人",
	})
	t.Cleanup(func() { cleanupAuthor(t, pool, other.Sub) })

	slug := submitHTML(t, h, sid, "另一个被移除作品", "<h1>y</h1>")
	publishNow(t, pool, slug)
	if _, err := pool.Exec(context.Background(), `
UPDATE demos SET lifecycle_state='removed', moderation_state='approved', updated_at=GREATEST(now(), created_at)
WHERE slug=$1`, slug); err != nil {
		t.Fatalf("置为 removed 失败: %v", err)
	}

	// 非作者读写都 404。
	for _, req := range []*http.Request{
		appealReq(http.MethodGet, "/api/v1/demos/"+slug+"/appeal", otherSID, ""),
		appealReq(http.MethodPost, "/api/v1/demos/"+slug+"/appeal", otherSID, `{"reason":"蹭一下"}`),
	} {
		rec := httptest.NewRecorder()
		if req.Method == http.MethodGet {
			h.GetDemoAppeal(rec, req, slug)
		} else {
			h.SubmitDemoAppeal(rec, req, slug)
		}
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s 非作者 = %d，想要 404", req.Method, rec.Code)
		}
	}

	// 未登录 401。
	rec := httptest.NewRecorder()
	h.GetDemoAppeal(rec, appealReq(http.MethodGet, "/api/v1/demos/"+slug+"/appeal", "", ""), slug)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("未登录 = %d，想要 401", rec.Code)
	}

	// 未移除的作品不能申诉。
	normal := submitHTML(t, h, sid, "正常作品", "<h1>z</h1>")
	publishNow(t, pool, normal)
	rec = httptest.NewRecorder()
	h.SubmitDemoAppeal(rec, appealReq(http.MethodPost, "/api/v1/demos/"+normal+"/appeal", sid,
		`{"reason":"不该能提"}`), normal)
	if rec.Code != http.StatusConflict {
		t.Fatalf("正常作品申诉 = %d，想要 409", rec.Code)
	}
}
