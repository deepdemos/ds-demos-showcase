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

// 提案审核（#433 / ENT-016 – ENT-024）：看得清、能驳回、不能假通过。
//
// 驳回的判据不是「接口 200」，而是：status 变了、原因落库、审计有一条、
// 正式业务表零变化。approve 的执行器没接入前必须 422。

func proposalReq(method, path, sid, body string) *http.Request {
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

func TestIntegrationProposalReviewReject(t *testing.T) {
	h, _, idSvc := newIdempotencyHandler(t)
	pool := integrationPool(t)
	adminSID := mkAdmin(t, pool, idSvc, "proposal")

	var proposalID int64
	if err := pool.QueryRow(context.Background(), `
INSERT INTO proposals (kind, payload, source)
VALUES ('new_task', '{"title":"提案驳回用例"}'::jsonb, 'user')
RETURNING id`).Scan(&proposalID); err != nil {
		t.Fatalf("插入提案失败: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM proposals WHERE id = $1`, proposalID)
	})

	// 列表：默认 pending，能按 kind / source 筛。
	rec := httptest.NewRecorder()
	h.ListAdminProposals(rec, proposalReq(http.MethodGet, "/api/v1/admin/proposals?kind=new_task&source=user", adminSID, ""),
		api.ListAdminProposalsParams{})
	if rec.Code != http.StatusOK {
		t.Fatalf("提案列表 = %d，想要 200（body=%s）", rec.Code, rec.Body.String())
	}
	var list api.ProposalList
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("提案列表不是合法 JSON: %v", err)
	}
	found := false
	for _, item := range list.Items {
		if item.Id == int(proposalID) && item.Title == "提案驳回用例" {
			found = true
		}
	}
	if !found {
		t.Fatalf("列表里没有新提案：%+v", list.Items)
	}

	// 详情：payload 原文可见。
	rec = httptest.NewRecorder()
	h.GetAdminProposal(rec, proposalReq(http.MethodGet, "/api/v1/admin/proposals/"+itoa(int(proposalID)), adminSID, ""), int(proposalID))
	if rec.Code != http.StatusOK {
		t.Fatalf("提案详情 = %d，想要 200（body=%s）", rec.Code, rec.Body.String())
	}
	var detail api.ProposalDetail
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatalf("提案详情不是合法 JSON: %v", err)
	}
	if detail.Status != api.ProposalDetailStatusPending || detail.Payload == nil {
		t.Fatalf("详情 = %+v", detail)
	}

	// 驳回：状态、原因、审计。
	rec = httptest.NewRecorder()
	h.ReviewAdminProposal(rec, proposalReq(http.MethodPost, "/api/v1/admin/proposals/x/review", adminSID,
		`{"decision":"reject","reason":"重复提案"}`), int(proposalID))
	if rec.Code != http.StatusOK {
		t.Fatalf("驳回 = %d，想要 200（body=%s）", rec.Code, rec.Body.String())
	}
	var result api.ProposalReviewResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("驳回结果不是合法 JSON: %v", err)
	}
	if result.Status != api.ProposalReviewResultStatusRejected {
		t.Fatalf("驳回结果 = %+v", result)
	}

	var status, reason string
	if err := pool.QueryRow(context.Background(), `SELECT status, COALESCE(reject_reason,'') FROM proposals WHERE id = $1`, proposalID).Scan(&status, &reason); err != nil {
		t.Fatalf("读提案失败: %v", err)
	}
	if status != "rejected" || reason != "重复提案" {
		t.Fatalf("落库 = %s / %q，想要 rejected / 重复提案", status, reason)
	}
	var audits int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_log WHERE entity_type='proposal' AND entity_id=$1`, proposalID).Scan(&audits); err != nil || audits < 1 {
		t.Fatalf("审计条数 = %d err=%v，想要 ≥1", audits, err)
	}

	// 重复驳回：409，不是 200。
	rec = httptest.NewRecorder()
	h.ReviewAdminProposal(rec, proposalReq(http.MethodPost, "/api/v1/admin/proposals/x/review", adminSID,
		`{"decision":"reject"}`), int(proposalID))
	if rec.Code != http.StatusConflict {
		t.Fatalf("重复驳回 = %d，想要 409", rec.Code)
	}
}

func TestIntegrationProposalApproveNotWired(t *testing.T) {
	h, _, idSvc := newIdempotencyHandler(t)
	pool := integrationPool(t)
	adminSID := mkAdmin(t, pool, idSvc, "papprove")

	var proposalID int64
	if err := pool.QueryRow(context.Background(), `
INSERT INTO proposals (kind, payload, source)
VALUES ('new_task', '{"title":"提案通过未接入"}'::jsonb, 'user')
RETURNING id`).Scan(&proposalID); err != nil {
		t.Fatalf("插入提案失败: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM proposals WHERE id = $1`, proposalID)
	})

	rec := httptest.NewRecorder()
	h.ReviewAdminProposal(rec, proposalReq(http.MethodPost, "/api/v1/admin/proposals/x/review", adminSID,
		`{"decision":"approve"}`), int(proposalID))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("通过未接入 = %d，想要 422（body=%s）", rec.Code, rec.Body.String())
	}
	var status string
	if err := pool.QueryRow(context.Background(), `SELECT status FROM proposals WHERE id = $1`, proposalID).Scan(&status); err != nil {
		t.Fatalf("读提案失败: %v", err)
	}
	if status != "pending" {
		t.Fatalf("通过失败后 status = %s，必须仍是 pending", status)
	}
}

func TestIntegrationProposalsRequirePermission(t *testing.T) {
	h, _, idSvc := newIdempotencyHandler(t)
	pool := integrationPool(t)
	plain, plainSID := registerVerified(t, pool, idSvc, identity.RegisterInput{
		Email:       "it-proposal-plain@example.com",
		Password:    "correct-horse-battery",
		Username:    "it-proposal-plain",
		DisplayName: "普通用户",
	})
	t.Cleanup(func() { cleanupAuthor(t, pool, plain.Sub) })

	rec := httptest.NewRecorder()
	h.ListAdminProposals(rec, proposalReq(http.MethodGet, "/api/v1/admin/proposals", plainSID, ""), api.ListAdminProposalsParams{})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("普通用户列表 = %d，想要 403", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.ListAdminProposals(rec, proposalReq(http.MethodGet, "/api/v1/admin/proposals", "", ""), api.ListAdminProposalsParams{})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("未登录列表 = %d，想要 401", rec.Code)
	}

	// 详情与驳回同样要 proposal.review，不能只挡列表。
	rec = httptest.NewRecorder()
	h.GetAdminProposal(rec, proposalReq(http.MethodGet, "/api/v1/admin/proposals/1", plainSID, ""), 1)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("普通用户详情 = %d，想要 403", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.ReviewAdminProposal(rec, proposalReq(http.MethodPost, "/api/v1/admin/proposals/1/review", plainSID,
		`{"decision":"reject"}`), 1)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("普通用户驳回 = %d，想要 403", rec.Code)
	}
}
