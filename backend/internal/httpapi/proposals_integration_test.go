package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/entity"
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
VALUES ('new_prompt', '{"title":"未接入提示词提案","content":"测试提示词内容"}'::jsonb, 'user')
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

func TestIntegrationProposalApproveClassification(t *testing.T) {
	h, _, idSvc := newIdempotencyHandler(t)
	pool := integrationPool(t)
	adminSID := mkAdmin(t, pool, idSvc, "pclass")

	slug := "it-executor-classification"
	if _, err := pool.Exec(context.Background(), `DELETE FROM classifications WHERE dimension = 'subject' AND slug = $1`, slug); err != nil {
		t.Fatalf("清理旧分类失败: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM classifications WHERE dimension = 'subject' AND slug = $1`, slug)
	})

	insert := func(label string) int64 {
		var id int64
		if err := pool.QueryRow(context.Background(), `
INSERT INTO proposals (kind, payload, source)
VALUES ('new_classification', jsonb_build_object('dimension','subject','slug',$1::text,'label',$2::text), 'user')
RETURNING id`, slug, label).Scan(&id); err != nil {
			t.Fatalf("插入提案失败: %v", err)
		}
		t.Cleanup(func() {
			_, _ = pool.Exec(context.Background(), `DELETE FROM proposals WHERE id = $1`, id)
		})
		return id
	}

	firstID := insert("执行器分类")
	rec := httptest.NewRecorder()
	h.ReviewAdminProposal(rec, proposalReq(http.MethodPost, "/api/v1/admin/proposals/x/review", adminSID,
		`{"decision":"approve"}`), int(firstID))
	if rec.Code != http.StatusOK {
		t.Fatalf("通过分类提案 = %d，想要 200（body=%s）", rec.Code, rec.Body.String())
	}
	var result api.ProposalReviewResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("通过结果不是合法 JSON: %v", err)
	}
	if result.Status != api.ProposalReviewResultStatusApproved {
		t.Fatalf("通过结果 = %+v", result)
	}

	var status, resultKind string
	var resultID int64
	if err := pool.QueryRow(context.Background(), `
SELECT status, COALESCE(result_kind,''), COALESCE(result_id,0)
FROM proposals WHERE id = $1`, firstID).Scan(&status, &resultKind, &resultID); err != nil {
		t.Fatalf("读提案失败: %v", err)
	}
	if status != "approved" || resultKind != "classification" || resultID == 0 {
		t.Fatalf("提案 = %s / %s / %d，想要 approved / classification / 非零", status, resultKind, resultID)
	}
	var classificationID int64
	var label string
	if err := pool.QueryRow(context.Background(), `
SELECT id, label FROM classifications WHERE dimension='subject' AND slug=$1`, slug).Scan(&classificationID, &label); err != nil {
		t.Fatalf("分类未落库: %v", err)
	}
	if classificationID != resultID || label != "执行器分类" {
		t.Fatalf("分类 = %d/%q，提案 result = %d", classificationID, label, resultID)
	}

	// 第二次通过同一提案：409。
	rec = httptest.NewRecorder()
	h.ReviewAdminProposal(rec, proposalReq(http.MethodPost, "/api/v1/admin/proposals/x/review", adminSID,
		`{"decision":"approve"}`), int(firstID))
	if rec.Code != http.StatusConflict {
		t.Fatalf("重复通过 = %d，想要 409", rec.Code)
	}

	// 目标已存在：再提一条同 slug，通过时复用 id、不覆盖已有 label（ENT-021）。
	secondID := insert("新名字不该覆盖")
	rec = httptest.NewRecorder()
	h.ReviewAdminProposal(rec, proposalReq(http.MethodPost, "/api/v1/admin/proposals/x/review", adminSID,
		`{"decision":"approve"}`), int(secondID))
	if rec.Code != http.StatusOK {
		t.Fatalf("目标已存在时通过 = %d，想要 200（body=%s）", rec.Code, rec.Body.String())
	}
	if err := pool.QueryRow(context.Background(), `SELECT label FROM classifications WHERE dimension='subject' AND slug=$1`, slug).Scan(&label); err != nil {
		t.Fatalf("读分类失败: %v", err)
	}
	if label != "执行器分类" {
		t.Fatalf("已有分类被覆盖成 %q", label)
	}
}

func TestIntegrationProposalApproveTask(t *testing.T) {
	h, _, idSvc := newIdempotencyHandler(t)
	pool := integrationPool(t)
	adminSID := mkAdmin(t, pool, idSvc, "ptask")

	title := "执行器题目"
	titleKey := entity.TitleKey(title)
	_, _ = pool.Exec(context.Background(), `DELETE FROM tasks WHERE title_key = $1`, titleKey)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM tasks WHERE title_key = $1`, titleKey)
	})

	insert := func(taskTitle string) int64 {
		var id int64
		if err := pool.QueryRow(context.Background(), `
INSERT INTO proposals (kind, payload, source)
VALUES ('new_task', jsonb_build_object('title',$1::text,'brief','测一下'), 'user')
RETURNING id`, taskTitle).Scan(&id); err != nil {
			t.Fatalf("插入提案失败: %v", err)
		}
		t.Cleanup(func() {
			_, _ = pool.Exec(context.Background(), `DELETE FROM proposals WHERE id = $1`, id)
		})
		return id
	}

	firstID := insert(title)
	rec := httptest.NewRecorder()
	h.ReviewAdminProposal(rec, proposalReq(http.MethodPost, "/api/v1/admin/proposals/x/review", adminSID,
		`{"decision":"approve"}`), int(firstID))
	if rec.Code != http.StatusOK {
		t.Fatalf("通过题目提案 = %d，想要 200（body=%s）", rec.Code, rec.Body.String())
	}

	var status, resultKind, gotTitle, gotNorm, brief string
	var resultID int64
	if err := pool.QueryRow(context.Background(), `
SELECT status, COALESCE(result_kind,''), COALESCE(result_id,0) FROM proposals WHERE id=$1`, firstID).Scan(&status, &resultKind, &resultID); err != nil {
		t.Fatalf("读提案失败: %v", err)
	}
	if status != "approved" || resultKind != "task" || resultID == 0 {
		t.Fatalf("提案 = %s / %s / %d", status, resultKind, resultID)
	}
	if err := pool.QueryRow(context.Background(), `
SELECT title, title_norm, brief FROM tasks WHERE id=$1`, resultID).Scan(&gotTitle, &gotNorm, &brief); err != nil {
		t.Fatalf("题目未落库: %v", err)
	}
	if gotTitle != title || gotNorm != entity.TitleNorm(title) || brief != "测一下" {
		t.Fatalf("题目 = %q / %q / %q", gotTitle, gotNorm, brief)
	}

	// 同名题（标点不同，title_key 相同）复用，不新建。
	secondID := insert(title + "！")
	rec = httptest.NewRecorder()
	h.ReviewAdminProposal(rec, proposalReq(http.MethodPost, "/api/v1/admin/proposals/x/review", adminSID,
		`{"decision":"approve"}`), int(secondID))
	if rec.Code != http.StatusOK {
		t.Fatalf("复用题目 = %d，想要 200（body=%s）", rec.Code, rec.Body.String())
	}
	var count int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM tasks WHERE title_key=$1 AND deleted_at IS NULL`, titleKey).Scan(&count); err != nil || count != 1 {
		t.Fatalf("同名题 count=%d err=%v，想要 1", count, err)
	}
}

func TestIntegrationProposalApproveSkill(t *testing.T) {
	h, _, idSvc := newIdempotencyHandler(t)
	pool := integrationPool(t)
	adminSID := mkAdmin(t, pool, idSvc, "pskill")

	slug := "it-executor-skill"
	_, _ = pool.Exec(context.Background(), `DELETE FROM skills WHERE slug = $1`, slug)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM skills WHERE slug = $1`, slug)
	})

	insert := func(name string) int64 {
		var id int64
		if err := pool.QueryRow(context.Background(), `
INSERT INTO proposals (kind, payload, source)
VALUES ('new_skill', jsonb_build_object('slug',$1::text,'name',$2::text,'url','https://example.com/s'), 'user')
RETURNING id`, slug, name).Scan(&id); err != nil {
			t.Fatalf("插入提案失败: %v", err)
		}
		t.Cleanup(func() {
			_, _ = pool.Exec(context.Background(), `DELETE FROM proposals WHERE id = $1`, id)
		})
		return id
	}

	firstID := insert("技能一")
	rec := httptest.NewRecorder()
	h.ReviewAdminProposal(rec, proposalReq(http.MethodPost, "/api/v1/admin/proposals/x/review", adminSID,
		`{"decision":"approve"}`), int(firstID))
	if rec.Code != http.StatusOK {
		t.Fatalf("通过技能提案 = %d，想要 200（body=%s）", rec.Code, rec.Body.String())
	}
	var status, resultKind, name string
	var resultID int64
	if err := pool.QueryRow(context.Background(), `
SELECT status, COALESCE(result_kind,''), COALESCE(result_id,0) FROM proposals WHERE id=$1`, firstID).Scan(&status, &resultKind, &resultID); err != nil {
		t.Fatalf("读提案失败: %v", err)
	}
	if status != "approved" || resultKind != "skill" || resultID == 0 {
		t.Fatalf("提案 = %s / %s / %d", status, resultKind, resultID)
	}
	if err := pool.QueryRow(context.Background(), `SELECT name FROM skills WHERE id=$1`, resultID).Scan(&name); err != nil || name != "技能一" {
		t.Fatalf("技能 = %q err=%v", name, err)
	}

	// 目标已存在：复用，不覆盖 name。
	secondID := insert("新名字不该覆盖")
	rec = httptest.NewRecorder()
	h.ReviewAdminProposal(rec, proposalReq(http.MethodPost, "/api/v1/admin/proposals/x/review", adminSID,
		`{"decision":"approve"}`), int(secondID))
	if rec.Code != http.StatusOK {
		t.Fatalf("复用技能 = %d，想要 200（body=%s）", rec.Code, rec.Body.String())
	}
	if err := pool.QueryRow(context.Background(), `SELECT name FROM skills WHERE slug=$1`, slug).Scan(&name); err != nil || name != "技能一" {
		t.Fatalf("已有技能被覆盖成 %q err=%v", name, err)
	}
}

func TestIntegrationProposalApproveSkillBadPayload(t *testing.T) {
	h, _, idSvc := newIdempotencyHandler(t)
	pool := integrationPool(t)
	adminSID := mkAdmin(t, pool, idSvc, "pskillbad")

	var proposalID int64
	if err := pool.QueryRow(context.Background(), `
INSERT INTO proposals (kind, payload, source)
VALUES ('new_skill', '{"slug":"bad-skill","name":"坏技能","spec":"bogus"}'::jsonb, 'user')
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
		t.Fatalf("非法 spec = %d，想要 422", rec.Code)
	}
	var status string
	if err := pool.QueryRow(context.Background(), `SELECT status FROM proposals WHERE id=$1`, proposalID).Scan(&status); err != nil || status != "pending" {
		t.Fatalf("非法 spec 后 status = %s err=%v，应保持 pending", status, err)
	}
}

func TestIntegrationProposalApproveClassificationBadPayload(t *testing.T) {
	h, _, idSvc := newIdempotencyHandler(t)
	pool := integrationPool(t)
	adminSID := mkAdmin(t, pool, idSvc, "pclassbad")

	var proposalID int64
	if err := pool.QueryRow(context.Background(), `
INSERT INTO proposals (kind, payload, source)
VALUES ('new_classification', '{"dimension":"bogus","slug":"bad"}'::jsonb, 'user')
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
		t.Fatalf("非法 dimension = %d，想要 422", rec.Code)
	}
	var status string
	if err := pool.QueryRow(context.Background(), `SELECT status FROM proposals WHERE id=$1`, proposalID).Scan(&status); err != nil || status != "pending" {
		t.Fatalf("非法 payload 后 status = %s err=%v，应保持 pending", status, err)
	}
}

func TestIntegrationProposalBatch(t *testing.T) {
	h, _, idSvc := newIdempotencyHandler(t)
	pool := integrationPool(t)
	adminSID := mkAdmin(t, pool, idSvc, "pbatch")

	insert := func(kind, payload string) int64 {
		var id int64
		if err := pool.QueryRow(context.Background(), `
INSERT INTO proposals (kind, payload, source) VALUES ($1, $2::jsonb, 'user') RETURNING id`, kind, payload).Scan(&id); err != nil {
			t.Fatalf("插入提案失败: %v", err)
		}
		t.Cleanup(func() {
			_, _ = pool.Exec(context.Background(), `DELETE FROM proposals WHERE id = $1`, id)
		})
		return id
	}

	slugA := "it-batch-skill-a"
	slugB := "it-batch-skill-b"
	_, _ = pool.Exec(context.Background(), `DELETE FROM skills WHERE slug IN ($1,$2)`, slugA, slugB)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM skills WHERE slug IN ($1,$2)`, slugA, slugB)
	})

	first := insert("new_skill", `{"slug":"`+slugA+`","name":"批量技能 A"}`)
	second := insert("new_skill", `{"slug":"`+slugB+`","name":"批量技能 B"}`)
	third := insert("new_model", `{"name":"批量模型"}`)

	// 先单条驳回 third，制造一行「已经处理过」。
	rec := httptest.NewRecorder()
	h.ReviewAdminProposal(rec, proposalReq(http.MethodPost, "/api/v1/admin/proposals/x/review", adminSID,
		`{"decision":"reject","reason":"先处理"}`), int(third))
	if rec.Code != http.StatusOK {
		t.Fatalf("预驳回 = %d", rec.Code)
	}

	body := `{"ids":[` + itoa(int(first)) + `,` + itoa(int(second)) + `,` + itoa(int(third)) + `],"decision":"reject","reason":"批量驳回"}`
	rec = httptest.NewRecorder()
	h.BatchReviewAdminProposals(rec, proposalReq(http.MethodPost, "/api/v1/admin/proposals/batch", adminSID, body))
	if rec.Code != http.StatusOK {
		t.Fatalf("批量驳回 = %d，想要 200（body=%s）", rec.Code, rec.Body.String())
	}
	var out api.ProposalBatchResult
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("批量结果不是合法 JSON: %v", err)
	}
	if len(out.Results) != 3 {
		t.Fatalf("结果条数 = %d，想要 3", len(out.Results))
	}
	if !out.Results[0].Ok || !out.Results[1].Ok || out.Results[2].Ok {
		t.Fatalf("逐条结果 = %+v，想要 前两条 ok、第三条失败", out.Results)
	}
	if out.Results[2].Error == nil || *out.Results[2].Error == "" {
		t.Fatalf("失败行没有 error：%+v", out.Results[2])
	}

	// 批量通过：技能成功，未接入的 new_model 行失败，但不影响其它行。
	modelID := insert("new_model", `{"name":"批量模型二"}`)
	skillID := insert("new_skill", `{"slug":"it-batch-skill-c","name":"批量技能 C"}`)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM skills WHERE slug='it-batch-skill-c'`)
	})
	body = `{"ids":[` + itoa(int(skillID)) + `,` + itoa(int(modelID)) + `],"decision":"approve"}`
	rec = httptest.NewRecorder()
	h.BatchReviewAdminProposals(rec, proposalReq(http.MethodPost, "/api/v1/admin/proposals/batch", adminSID, body))
	if rec.Code != http.StatusOK {
		t.Fatalf("批量通过 = %d（body=%s）", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("批量通过结果不是合法 JSON: %v", err)
	}
	if !out.Results[0].Ok || out.Results[1].Ok {
		t.Fatalf("批量通过结果 = %+v", out.Results)
	}

	// 空 / 重复 ids：400。
	for _, bad := range []string{`{"ids":[],"decision":"reject"}`, `{"ids":[1,1],"decision":"reject"}`} {
		rec = httptest.NewRecorder()
		h.BatchReviewAdminProposals(rec, proposalReq(http.MethodPost, "/api/v1/admin/proposals/batch", adminSID, bad))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("非法 ids %s = %d，想要 400", bad, rec.Code)
		}
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
