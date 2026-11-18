package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/identity"
)

// 通知域（#437 / NTF-001 – NTF-008）。
//
// 钉三件事：审核 / 评分真的会写通知；已读与未读的口径；读不到别人的通知。

func notifReq(method, path, sid string) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	if sid != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sid})
	}
	return req
}

func listNotifications(t *testing.T, h *Handler, sid string) api.NotificationList {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ListNotifications(rec, notifReq(http.MethodGet, "/api/v1/notifications", sid), api.ListNotificationsParams{})
	if rec.Code != http.StatusOK {
		t.Fatalf("通知列表 = %d，想要 200（body=%s）", rec.Code, rec.Body.String())
	}
	var out api.NotificationList
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("通知不是合法 JSON: %v", err)
	}
	return out
}

func reviewDemo(t *testing.T, h *Handler, sid, slug, decision, reason string) {
	t.Helper()
	payload := map[string]string{"decision": decision}
	if reason != "" {
		payload["reason"] = reason
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("序列化裁决失败: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/reviews/demo/"+slug, bytes.NewReader(body))
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sid})
	rec := httptest.NewRecorder()
	h.ReviewDemo(rec, req, slug)
	if rec.Code != http.StatusOK {
		t.Fatalf("审核 = %d，想要 200（body=%s）", rec.Code, rec.Body.String())
	}
}

func TestIntegrationNotificationsFromReviewAndRating(t *testing.T) {
	h, _, idSvc := newIdempotencyHandler(t)
	pool := integrationPool(t)

	author, authorSID := registerVerified(t, pool, idSvc, identity.RegisterInput{
		Email:       "it-notif-author@example.com",
		Password:    "correct-horse-battery",
		Username:    "it-notif-author",
		DisplayName: "通知作者",
	})
	t.Cleanup(func() { cleanupAuthor(t, pool, author.Sub) })
	adminSID := mkAdmin(t, pool, idSvc, "notif")

	slug := submitHTML(t, h, authorSID, "通知作品", "<h1>notify</h1>")
	reviewDemo(t, h, adminSID, slug, "approve", "")

	got := listNotifications(t, h, authorSID)
	if got.Total != 1 || got.UnreadCount != 1 {
		t.Fatalf("审核通过后通知 = total %d / unread %d，想要 1/1", got.Total, got.UnreadCount)
	}
	if got.Items[0].Kind != "review.approved" || got.Items[0].DemoSlug != slug {
		t.Fatalf("通知 = %+v", got.Items[0])
	}
	// 通过但还没发布：作品不可访问，前端要能显示说明（NTF-007）。
	if got.Items[0].Accessible {
		t.Fatal("未发布作品的通知不该标记为 accessible")
	}

	publishNow(t, pool, slug)

	// 别人打分 → 作者收到一条；自己给自己打分不再多。
	rater, raterSID := registerVerified(t, pool, idSvc, identity.RegisterInput{
		Email:       "it-notif-rater@example.com",
		Password:    "correct-horse-battery",
		Username:    "it-notif-rater",
		DisplayName: "打分人",
	})
	t.Cleanup(func() { cleanupAuthor(t, pool, rater.Sub) })
	if rec := putRating(t, h, slug, raterSID, 4); rec.Code != http.StatusOK {
		t.Fatalf("别人打分 = %d，想要 200", rec.Code)
	}
	if rec := putRating(t, h, slug, authorSID, 5); rec.Code != http.StatusOK {
		t.Fatalf("自己打分 = %d，想要 200", rec.Code)
	}

	got = listNotifications(t, h, authorSID)
	if got.Total != 2 || got.UnreadCount != 2 {
		t.Fatalf("打分后通知 = total %d / unread %d，想要 2/2（自己的评分不该通知自己）", got.Total, got.UnreadCount)
	}
	ratingKind := ""
	for _, item := range got.Items {
		if item.Kind == "rating.received" {
			ratingKind = item.Kind
			if item.Payload["score"] != float64(4) {
				t.Fatalf("评分通知 payload = %+v，想要 score=4", item.Payload)
			}
		}
	}
	if ratingKind == "" {
		t.Fatal("没有收到评分通知")
	}

	// 已读：单项幂等、全部已读、未读数归零。
	firstID := got.Items[0].Id
	for i := 0; i < 2; i++ {
		rec := httptest.NewRecorder()
		h.MarkNotificationRead(rec, notifReq(http.MethodPost, "/api/v1/notifications/x/read", authorSID), firstID)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("第 %d 次标记已读 = %d，想要 204", i+1, rec.Code)
		}
	}
	rec := httptest.NewRecorder()
	h.MarkAllNotificationsRead(rec, notifReq(http.MethodPost, "/api/v1/notifications/read-all", authorSID))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("全部已读 = %d，想要 204", rec.Code)
	}
	if unread := unreadCount(t, h, authorSID); unread != 0 {
		t.Fatalf("全部已读后 unread = %d，想要 0", unread)
	}

	// 别人的通知读不到、标记不了。
	if other := listNotifications(t, h, raterSID); other.Total != 0 {
		t.Fatalf("路人通知 = %d，想要 0", other.Total)
	}
	rec = httptest.NewRecorder()
	h.MarkNotificationRead(rec, notifReq(http.MethodPost, "/api/v1/notifications/x/read", raterSID), firstID)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("标记别人的通知 = %d，想要 404", rec.Code)
	}
}

func TestIntegrationNotificationRejectedReason(t *testing.T) {
	h, _, idSvc := newIdempotencyHandler(t)
	pool := integrationPool(t)

	author, sid := registerVerified(t, pool, idSvc, identity.RegisterInput{
		Email:       "it-notif-reject@example.com",
		Password:    "correct-horse-battery",
		Username:    "it-notif-reject",
		DisplayName: "被驳回作者",
	})
	t.Cleanup(func() { cleanupAuthor(t, pool, author.Sub) })
	adminSID := mkAdmin(t, pool, idSvc, "notif-reject")

	slug := submitHTML(t, h, sid, "驳回作品", "<h1>reject</h1>")
	reviewDemo(t, h, adminSID, slug, "reject", "封面不合格")

	got := listNotifications(t, h, sid)
	if got.Total != 1 || got.Items[0].Kind != "review.rejected" {
		t.Fatalf("驳回通知 = %+v", got.Items)
	}
	if got.Items[0].Payload["reason"] != "封面不合格" {
		t.Fatalf("驳回理由 = %+v", got.Items[0].Payload)
	}
}

func TestIntegrationNotificationRetention(t *testing.T) {
	h, _, idSvc := newIdempotencyHandler(t)
	pool := integrationPool(t)

	account, sid := registerVerified(t, pool, idSvc, identity.RegisterInput{
		Email:       "it-notif-retention@example.com",
		Password:    "correct-horse-battery",
		Username:    "it-notif-retention",
		DisplayName: "过期用户",
	})
	t.Cleanup(func() { cleanupAuthor(t, pool, account.Sub) })

	if _, err := pool.Exec(context.Background(), `
INSERT INTO notifications (account_id, kind, demo_slug, demo_title, created_at)
SELECT id, 'rating.received', 'old-demo', '旧通知', now() - interval '100 days'
FROM accounts WHERE sub = $1`, account.Sub); err != nil {
		t.Fatalf("插入过期通知失败: %v", err)
	}

	got := listNotifications(t, h, sid)
	if got.Total != 0 {
		t.Fatalf("100 天前的通知应被清理，实际 total=%d", got.Total)
	}
}

func TestIntegrationNotificationsRequireLogin(t *testing.T) {
	h, _, _ := newIdempotencyHandler(t)
	cases := []struct {
		name string
		do   func(*httptest.ResponseRecorder)
	}{
		{"列表", func(rec *httptest.ResponseRecorder) {
			h.ListNotifications(rec, notifReq(http.MethodGet, "/api/v1/notifications", ""), api.ListNotificationsParams{})
		}},
		{"未读数", func(rec *httptest.ResponseRecorder) {
			h.GetUnreadNotificationCount(rec, notifReq(http.MethodGet, "/api/v1/notifications/unread-count", ""))
		}},
		{"标记已读", func(rec *httptest.ResponseRecorder) {
			h.MarkNotificationRead(rec, notifReq(http.MethodPost, "/api/v1/notifications/1/read", ""), 1)
		}},
		{"全部已读", func(rec *httptest.ResponseRecorder) {
			h.MarkAllNotificationsRead(rec, notifReq(http.MethodPost, "/api/v1/notifications/read-all", ""))
		}},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		tc.do(rec)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s 未登录 = %d，想要 401", tc.name, rec.Code)
		}
	}
}

func unreadCount(t *testing.T, h *Handler, sid string) int {
	t.Helper()
	rec := httptest.NewRecorder()
	h.GetUnreadNotificationCount(rec, notifReq(http.MethodGet, "/api/v1/notifications/unread-count", sid))
	if rec.Code != http.StatusOK {
		t.Fatalf("未读数 = %d，想要 200", rec.Code)
	}
	var out api.UnreadCount
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("未读数不是合法 JSON: %v", err)
	}
	return out.Count
}
