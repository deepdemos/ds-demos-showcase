package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/identity"
)

// 管理员申诉裁决（#463 第二半）：通过恢复作品、驳回保持 removed、状态变了不硬通过。

func TestIntegrationAdminAppealApprove(t *testing.T) {
	h, _, idSvc := newIdempotencyHandler(t)
	pool := integrationPool(t)

	author, sid := registerVerified(t, pool, idSvc, identity.RegisterInput{
		Email:       "it-appeal-admin@example.com",
		Password:    "correct-horse-battery",
		Username:    "it-appeal-admin",
		DisplayName: "申诉者",
	})
	t.Cleanup(func() { cleanupAuthor(t, pool, author.Sub) })
	adminSID := mkAdmin(t, pool, idSvc, "appealadm")

	slug := submitHTML(t, h, sid, "待申诉作品", "<h1>a</h1>")
	publishNow(t, pool, slug)
	if _, err := pool.Exec(context.Background(), `
UPDATE demos SET lifecycle_state='removed', moderation_state='approved', updated_at=GREATEST(now(), created_at)
WHERE slug=$1`, slug); err != nil {
		t.Fatalf("置 removed 失败: %v", err)
	}

	rec := httptest.NewRecorder()
	h.SubmitDemoAppeal(rec, appealReq(http.MethodPost, "/api/v1/demos/"+slug+"/appeal", sid,
		`{"reason":"请复核"}`), slug)
	if rec.Code != http.StatusCreated {
		t.Fatalf("提交申诉 = %d（body=%s）", rec.Code, rec.Body.String())
	}
	var appeal api.DemoAppeal
	if err := json.Unmarshal(rec.Body.Bytes(), &appeal); err != nil {
		t.Fatalf("申诉不是合法 JSON: %v", err)
	}

	// 列表能看见。
	rec = httptest.NewRecorder()
	h.ListAdminAppeals(rec, appealReq(http.MethodGet, "/api/v1/admin/appeals", adminSID, ""), api.ListAdminAppealsParams{})
	if rec.Code != http.StatusOK {
		t.Fatalf("申诉列表 = %d（body=%s）", rec.Code, rec.Body.String())
	}
	var list api.AdminAppealList
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("列表不是合法 JSON: %v", err)
	}
	found := false
	for _, item := range list.Items {
		if item.Id == appeal.Id && item.DemoSlug == slug {
			found = true
		}
	}
	if !found {
		t.Fatalf("列表里没有新申诉：%+v", list.Items)
	}

	// 通过：恢复 published + 通知作者 + 审计。
	rec = httptest.NewRecorder()
	h.ReviewAdminAppeal(rec, appealReq(http.MethodPost, "/api/v1/admin/appeals/x/review", adminSID,
		`{"decision":"approve","reason":"误判，恢复"}`), appeal.Id)
	if rec.Code != http.StatusOK {
		t.Fatalf("通过申诉 = %d（body=%s）", rec.Code, rec.Body.String())
	}
	var result api.AppealReviewResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil || result.Status != api.AppealReviewResultStatusApproved {
		t.Fatalf("裁决结果 = %+v err=%v", result, err)
	}

	var lifecycle, status string
	if err := pool.QueryRow(context.Background(), `
SELECT d.lifecycle_state, a.status FROM demo_appeals a JOIN demos d ON d.id=a.demo_id WHERE a.id=$1`, appeal.Id).Scan(&lifecycle, &status); err != nil {
		t.Fatalf("读申诉失败: %v", err)
	}
	if lifecycle != "published" || status != "approved" {
		t.Fatalf("恢复结果 = %s / %s，想要 published / approved", lifecycle, status)
	}

	var notices, audits int
	if err := pool.QueryRow(context.Background(), `
SELECT count(*) FROM notifications n
JOIN accounts ac ON ac.id=n.account_id
WHERE ac.sub=$1 AND n.kind='appeal.approved'`, author.Sub).Scan(&notices); err != nil || notices < 1 {
		t.Fatalf("申诉通过通知 = %d err=%v", notices, err)
	}
	if err := pool.QueryRow(context.Background(), `
SELECT count(*) FROM audit_log WHERE entity_type='demo' AND entity_id=(SELECT demo_id FROM demo_appeals WHERE id=$1)`, appeal.Id).Scan(&audits); err != nil || audits < 1 {
		t.Fatalf("申诉审计 = %d err=%v", audits, err)
	}

	// 重复裁决 409。
	rec = httptest.NewRecorder()
	h.ReviewAdminAppeal(rec, appealReq(http.MethodPost, "/api/v1/admin/appeals/x/review", adminSID,
		`{"decision":"reject"}`), appeal.Id)
	if rec.Code != http.StatusConflict {
		t.Fatalf("重复裁决 = %d，想要 409", rec.Code)
	}
}

func TestIntegrationAdminAppealRejectAndStateConflict(t *testing.T) {
	h, _, idSvc := newIdempotencyHandler(t)
	pool := integrationPool(t)

	author, sid := registerVerified(t, pool, idSvc, identity.RegisterInput{
		Email:       "it-appeal-reject@example.com",
		Password:    "correct-horse-battery",
		Username:    "it-appeal-reject",
		DisplayName: "申诉者二",
	})
	t.Cleanup(func() { cleanupAuthor(t, pool, author.Sub) })
	adminSID := mkAdmin(t, pool, idSvc, "appealrej")

	slug := submitHTML(t, h, sid, "申诉驳回作品", "<h1>b</h1>")
	publishNow(t, pool, slug)
	if _, err := pool.Exec(context.Background(), `
UPDATE demos SET lifecycle_state='removed', moderation_state='approved', updated_at=GREATEST(now(), created_at)
WHERE slug=$1`, slug); err != nil {
		t.Fatalf("置 removed 失败: %v", err)
	}
	rec := httptest.NewRecorder()
	h.SubmitDemoAppeal(rec, appealReq(http.MethodPost, "/api/v1/demos/"+slug+"/appeal", sid,
		`{"reason":"再复核"}`), slug)
	var appeal api.DemoAppeal
	if err := json.Unmarshal(rec.Body.Bytes(), &appeal); err != nil {
		t.Fatalf("申诉不是合法 JSON: %v", err)
	}

	// 作品被别的路径改成 archived：通过必须 409，且申诉仍是 pending。
	if _, err := pool.Exec(context.Background(), `UPDATE demos SET lifecycle_state='archived' WHERE slug=$1`, slug); err != nil {
		t.Fatalf("改 archived 失败: %v", err)
	}
	rec = httptest.NewRecorder()
	h.ReviewAdminAppeal(rec, appealReq(http.MethodPost, "/api/v1/admin/appeals/x/review", adminSID,
		`{"decision":"approve"}`), appeal.Id)
	if rec.Code != http.StatusConflict {
		t.Fatalf("作品已 archived 时通过 = %d，想要 409（body=%s）", rec.Code, rec.Body.String())
	}
	var status string
	if err := pool.QueryRow(context.Background(), `SELECT status FROM demo_appeals WHERE id=$1`, appeal.Id).Scan(&status); err != nil || status != "pending" {
		t.Fatalf("409 后申诉 status = %s err=%v，应仍 pending", status, err)
	}

	// 改回 removed 后驳回：作品保持不变。
	if _, err := pool.Exec(context.Background(), `UPDATE demos SET lifecycle_state='removed' WHERE slug=$1`, slug); err != nil {
		t.Fatalf("改回 removed 失败: %v", err)
	}
	rec = httptest.NewRecorder()
	h.ReviewAdminAppeal(rec, appealReq(http.MethodPost, "/api/v1/admin/appeals/x/review", adminSID,
		`{"decision":"reject","reason":"维持移除"}`), appeal.Id)
	if rec.Code != http.StatusOK {
		t.Fatalf("驳回申诉 = %d（body=%s）", rec.Code, rec.Body.String())
	}
	var lifecycle, decision string
	if err := pool.QueryRow(context.Background(), `
SELECT d.lifecycle_state, a.decision_reason FROM demo_appeals a JOIN demos d ON d.id=a.demo_id WHERE a.id=$1`, appeal.Id).Scan(&lifecycle, &decision); err != nil {
		t.Fatalf("读驳回结果失败: %v", err)
	}
	if lifecycle != "removed" || decision != "维持移除" {
		t.Fatalf("驳回结果 = %s / %q", lifecycle, decision)
	}
}

func TestIntegrationAdminAppealsRequirePermission(t *testing.T) {
	h, _, idSvc := newIdempotencyHandler(t)
	pool := integrationPool(t)
	plain, plainSID := registerVerified(t, pool, idSvc, identity.RegisterInput{
		Email:       "it-appeal-plain@example.com",
		Password:    "correct-horse-battery",
		Username:    "it-appeal-plain",
		DisplayName: "普通用户",
	})
	t.Cleanup(func() { cleanupAuthor(t, pool, plain.Sub) })

	rec := httptest.NewRecorder()
	h.ListAdminAppeals(rec, appealReq(http.MethodGet, "/api/v1/admin/appeals", plainSID, ""), api.ListAdminAppealsParams{})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("普通用户列表 = %d，想要 403", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.ListAdminAppeals(rec, appealReq(http.MethodGet, "/api/v1/admin/appeals", "", ""), api.ListAdminAppealsParams{})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("未登录列表 = %d，想要 401", rec.Code)
	}
}
