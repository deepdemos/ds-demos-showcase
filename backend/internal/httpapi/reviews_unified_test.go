package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/identity"
)

// 统一裁决入口（GOV-006 / #433）：kind=demo 委托给现有裁决逻辑。
//
// 这里只钉「分派正确」与「未知 kind 不装成功」；裁决本身的语义由 reviews 的用例覆盖。

func unifiedReviewReq(sid, slug, body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/reviews/demo/"+slug, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if sid != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sid})
	}
	return req
}

func TestIntegrationUnifiedReviewDemo(t *testing.T) {
	h, _, idSvc := newIdempotencyHandler(t)
	pool := integrationPool(t)

	author, sid := registerVerified(t, pool, idSvc, identity.RegisterInput{
		Email:       "it-unified-review@example.com",
		Password:    "correct-horse-battery",
		Username:    "it-unified-review",
		DisplayName: "统一入口作者",
	})
	t.Cleanup(func() { cleanupAuthor(t, pool, author.Sub) })
	adminSID := mkAdmin(t, pool, idSvc, "unified")

	slug := submitHTML(t, h, sid, "统一入口作品", "<h1>unified</h1>")

	rec := httptest.NewRecorder()
	h.ReviewEntity(rec, unifiedReviewReq(adminSID, slug, `{"decision":"approve"}`), api.Demo, slug)
	if rec.Code != http.StatusOK {
		t.Fatalf("统一入口通过 = %d，想要 200（body=%s）", rec.Code, rec.Body.String())
	}
	var out api.ReviewResult
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("裁决结果不是合法 JSON: %v", err)
	}
	if out.Slug != slug || out.ModerationState != api.ReviewResultModerationStateApproved {
		t.Fatalf("裁决结果 = %+v", out)
	}

	// 未知 kind：400，不能因为「不等于 demo」就落到某个默认分支。
	rec = httptest.NewRecorder()
	h.ReviewEntity(rec, unifiedReviewReq(adminSID, slug, `{"decision":"reject"}`),
		api.ReviewEntityParamsKind("proposal"), slug)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("未知 kind = %d，想要 400（body=%s）", rec.Code, rec.Body.String())
	}

	// 未登录：401（由被委托的 ReviewDemo 返回）。
	rec = httptest.NewRecorder()
	h.ReviewEntity(rec, unifiedReviewReq("", slug, `{"decision":"reject"}`), api.Demo, slug)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("未登录 = %d，想要 401", rec.Code)
	}
}
