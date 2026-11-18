package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
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

func TestIntegrationUnifiedReviewProposal(t *testing.T) {
	h, _, idSvc := newIdempotencyHandler(t)
	pool := integrationPool(t)
	adminSID := mkAdmin(t, pool, idSvc, "uprup")

	var proposalID int64
	if err := pool.QueryRow(context.Background(), `
INSERT INTO proposals (kind, payload, source)
VALUES ('new_skill', '{"slug":"it-unified-proposal","name":"统一入口技能"}'::jsonb, 'user')
RETURNING id`).Scan(&proposalID); err != nil {
		t.Fatalf("插入提案失败: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM proposals WHERE id = $1`, proposalID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM skills WHERE slug = 'it-unified-proposal'`)
	})

	rec := httptest.NewRecorder()
	h.ReviewEntity(rec, unifiedReviewReq(adminSID, strconv.FormatInt(proposalID, 10), `{"decision":"reject","reason":"统一入口驳回"}`),
		api.ReviewEntityParamsKindProposal, strconv.FormatInt(proposalID, 10))
	if rec.Code != http.StatusOK {
		t.Fatalf("统一入口驳回提案 = %d，想要 200（body=%s）", rec.Code, rec.Body.String())
	}
	var out api.ReviewResult
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("裁决结果不是合法 JSON: %v", err)
	}
	if out.Slug != strconv.FormatInt(proposalID, 10) || out.ModerationState != api.ReviewResultModerationStateRejected {
		t.Fatalf("裁决结果 = %+v", out)
	}
	if out.Kind == nil || *out.Kind != api.ReviewResultKindProposal {
		t.Fatalf("kind = %v，想要 proposal", out.Kind)
	}

	var status, reason string
	if err := pool.QueryRow(context.Background(), `SELECT status, COALESCE(reject_reason,'') FROM proposals WHERE id=$1`, proposalID).Scan(&status, &reason); err != nil {
		t.Fatalf("读提案失败: %v", err)
	}
	if status != "rejected" || reason != "统一入口驳回" {
		t.Fatalf("提案 = %s / %q", status, reason)
	}
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
	h.ReviewEntity(rec, unifiedReviewReq(adminSID, slug, `{"decision":"approve"}`), api.ReviewEntityParamsKindDemo, slug)
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
		api.ReviewEntityParamsKind("bogus"), slug)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("未知 kind = %d，想要 400（body=%s）", rec.Code, rec.Body.String())
	}

	// 未登录：401（由被委托的 ReviewDemo 返回）。
	rec = httptest.NewRecorder()
	h.ReviewEntity(rec, unifiedReviewReq("", slug, `{"decision":"reject"}`), api.ReviewEntityParamsKindDemo, slug)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("未登录 = %d，想要 401", rec.Code)
	}
}
