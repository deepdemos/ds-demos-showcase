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

// 聚合收件箱（#433）：三组待办与各自的计数。
//
// 关键判据不是「有数据」，而是「计数与实际行数一致」——limit 截断列表但不能截断计数。

func inboxReq(sid string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/inbox", nil)
	if sid != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sid})
	}
	return req
}

func getInbox(t *testing.T, h *Handler, sid string, limit int) api.AdminInbox {
	t.Helper()
	params := api.GetAdminInboxParams{}
	if limit > 0 {
		params.Limit = &limit
	}
	rec := httptest.NewRecorder()
	h.GetAdminInbox(rec, inboxReq(sid), params)
	if rec.Code != http.StatusOK {
		t.Fatalf("收件箱 = %d，想要 200（body=%s）", rec.Code, rec.Body.String())
	}
	var out api.AdminInbox
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("收件箱不是合法 JSON: %v", err)
	}
	return out
}

func TestIntegrationAdminInbox(t *testing.T) {
	h, _, idSvc := newIdempotencyHandler(t)
	pool := integrationPool(t)

	author, authorSID := registerVerified(t, pool, idSvc, identity.RegisterInput{
		Email:       "it-inbox-author@example.com",
		Password:    "correct-horse-battery",
		Username:    "it-inbox-author",
		DisplayName: "收件箱作者",
	})
	t.Cleanup(func() { cleanupAuthor(t, pool, author.Sub) })
	adminSID := mkAdmin(t, pool, idSvc, "inbox")

	// 计数是全库的，套件里别的用例会留下待审作品——用「插入前后差值」判，不能写死 1。
	before := getInbox(t, h, adminSID, 0)

	pendingSlug := submitHTML(t, h, authorSID, "待审作品", "<h1>pending</h1>")
	approvedSlug := submitHTML(t, h, authorSID, "待发布作品", "<h1>approved</h1>")
	reviewDemo(t, h, adminSID, approvedSlug, "approve", "")

	if _, err := pool.Exec(context.Background(), `
INSERT INTO proposals (kind, payload, source)
VALUES ('new_task', '{"title":"提案标题"}'::jsonb, 'user')`); err != nil {
		t.Fatalf("插入提案失败: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM proposals WHERE payload->>'title' = '提案标题'`)
	})

	inbox := getInbox(t, h, adminSID, 100)
	if inbox.DemoPendingCount != before.DemoPendingCount+1 ||
		inbox.ApprovedUnpublishedCount != before.ApprovedUnpublishedCount+1 ||
		inbox.ProposalPendingCount != before.ProposalPendingCount+1 {
		t.Fatalf("计数 = %d/%d/%d，插入前 = %d/%d/%d（应各 +1）",
			inbox.DemoPendingCount, inbox.ApprovedUnpublishedCount, inbox.ProposalPendingCount,
			before.DemoPendingCount, before.ApprovedUnpublishedCount, before.ProposalPendingCount)
	}
	if !containsSlug(inbox.DemoPending, pendingSlug) {
		t.Fatalf("待审列表里没有 %s：%+v", pendingSlug, inbox.DemoPending)
	}
	if !containsSlug(inbox.ApprovedUnpublished, approvedSlug) {
		t.Fatalf("待发布列表里没有 %s：%+v", approvedSlug, inbox.ApprovedUnpublished)
	}
	foundProposal := false
	for _, proposal := range inbox.Proposals {
		if proposal.Title == "提案标题" {
			foundProposal = true
		}
	}
	if !foundProposal {
		t.Fatalf("提案列表里没有新建提案：%+v", inbox.Proposals)
	}

	// limit=1 只截列表，不截计数。
	limited := getInbox(t, h, adminSID, 1)
	if limited.DemoPendingCount != inbox.DemoPendingCount ||
		limited.ApprovedUnpublishedCount != inbox.ApprovedUnpublishedCount ||
		limited.ProposalPendingCount != inbox.ProposalPendingCount {
		t.Fatalf("limit=1 后计数变了：%d/%d/%d vs %d/%d/%d",
			limited.DemoPendingCount, limited.ApprovedUnpublishedCount, limited.ProposalPendingCount,
			inbox.DemoPendingCount, inbox.ApprovedUnpublishedCount, inbox.ProposalPendingCount)
	}
	if len(limited.DemoPending) > 1 || len(limited.ApprovedUnpublished) > 1 || len(limited.Proposals) > 1 {
		t.Fatalf("limit=1 列表没截断：%d/%d/%d",
			len(limited.DemoPending), len(limited.ApprovedUnpublished), len(limited.Proposals))
	}
}

func TestIntegrationAdminInboxRequiresPermission(t *testing.T) {
	h, _, idSvc := newIdempotencyHandler(t)
	pool := integrationPool(t)

	plain, plainSID := registerVerified(t, pool, idSvc, identity.RegisterInput{
		Email:       "it-inbox-plain@example.com",
		Password:    "correct-horse-battery",
		Username:    "it-inbox-plain",
		DisplayName: "普通用户",
	})
	t.Cleanup(func() { cleanupAuthor(t, pool, plain.Sub) })

	rec := httptest.NewRecorder()
	h.GetAdminInbox(rec, inboxReq(plainSID), api.GetAdminInboxParams{})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("普通用户 = %d，想要 403", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.GetAdminInbox(rec, inboxReq(""), api.GetAdminInboxParams{})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("未登录 = %d，想要 401", rec.Code)
	}
}

func containsSlug(items []api.ReviewQueueItem, slug string) bool {
	for _, item := range items {
		if item.Slug == slug {
			return true
		}
	}
	return false
}
