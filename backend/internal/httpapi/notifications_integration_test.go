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
	// 对象三列与旧字段同值：作品类事件在写入路径上写的是同一份快照。
	first := got.Items[0]
	if first.ObjectType != "demo" || first.ObjectKey != slug || first.ObjectTitle != "通知作品" {
		t.Fatalf("对象三列 = %q / %q / %q，想要 demo / %s / 通知作品",
			first.ObjectType, first.ObjectKey, first.ObjectTitle, slug)
	}
	if first.DemoSlug != first.ObjectKey || first.DemoTitle != first.ObjectTitle {
		t.Fatalf("过渡期旧字段 = %q / %q，想要与对象字段同值", first.DemoSlug, first.DemoTitle)
	}
	// 旧列也还在写（回滚窗口内两边同值）；读路径已经从新列来。
	var dbSlug, dbTitle, dbType, dbKey string
	if err := pool.QueryRow(context.Background(), `
SELECT demo_slug, demo_title, object_type, object_key FROM notifications
WHERE account_id = (SELECT id FROM accounts WHERE sub = $1) AND kind = 'review.approved'`, author.Sub).
		Scan(&dbSlug, &dbTitle, &dbType, &dbKey); err != nil {
		t.Fatalf("读通知行失败: %v", err)
	}
	if dbSlug != slug || dbTitle != "通知作品" || dbType != "demo" || dbKey != slug {
		t.Fatalf("库里的旧列/新列 = %q / %q / %q / %q", dbSlug, dbTitle, dbType, dbKey)
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
INSERT INTO notifications (account_id, kind, object_type, object_key, object_title, created_at)
SELECT id, 'rating.received', 'demo', 'old-demo', '旧通知', now() - interval '100 days'
FROM accounts WHERE sub = $1`, account.Sub); err != nil {
		t.Fatalf("插入过期通知失败: %v", err)
	}

	got := listNotifications(t, h, sid)
	if got.Total != 0 {
		t.Fatalf("100 天前的通知应被清理，实际 total=%d", got.Total)
	}
}

// 非作品对象的通知：模型 / 提案这类事件没有作品 slug，所以对象改用「类型 + 键 + 标题快照」。
//
// 这条钉两件事：写入路径不查作品作者（接收者由调用方给），读路径把三类字段原样带回来；
// 旧字段只为作品类事件填充——非作品对象读回来是空串，而不是一个不存在的作品 slug。
func TestIntegrationNotificationNonDemoObject(t *testing.T) {
	h, _, idSvc := newIdempotencyHandler(t)
	pool := integrationPool(t)

	account, sid := registerVerified(t, pool, idSvc, identity.RegisterInput{
		Email:       "it-notif-model@example.com",
		Password:    "correct-horse-battery",
		Username:    "it-notif-model",
		DisplayName: "提案作者",
	})
	t.Cleanup(func() { cleanupAuthor(t, pool, account.Sub) })

	// 走真实的写入路径（#595 的驳回通知会走同一条）。
	h.notifyObject(context.Background(), account.ID, "model.proposal.rejected",
		notificationObject{Type: "proposal", Key: "42", Title: "示例收录提案"},
		map[string]string{"reason": "和已有条目重复"})

	got := listNotifications(t, h, sid)
	if got.Total != 1 || got.UnreadCount != 1 {
		t.Fatalf("非作品通知 = total %d / unread %d，想要 1/1", got.Total, got.UnreadCount)
	}
	item := got.Items[0]
	if item.Kind != "model.proposal.rejected" || item.ObjectType != "proposal" ||
		item.ObjectKey != "42" || item.ObjectTitle != "示例收录提案" {
		t.Fatalf("非作品通知 = %+v", item)
	}
	if item.DemoSlug != "" || item.DemoTitle != "" {
		t.Fatalf("非作品对象不该填作品字段，实际 %q / %q", item.DemoSlug, item.DemoTitle)
	}
	// 非作品对象不套作品公开可见性：对象键在就算可访问（过渡口径）。
	if !item.Accessible {
		t.Fatal("非作品对象不该被判成不可访问")
	}
	if item.Payload["reason"] != "和已有条目重复" {
		t.Fatalf("payload = %+v", item.Payload)
	}

	// 已读口径不受对象类型影响。
	rec := httptest.NewRecorder()
	h.MarkNotificationRead(rec, notifReq(http.MethodPost, "/api/v1/notifications/x/read", sid), item.Id)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("标记已读 = %d，想要 204", rec.Code)
	}
	if unread := unreadCount(t, h, sid); unread != 0 {
		t.Fatalf("已读后 unread = %d，想要 0", unread)
	}
}

// 对象被删掉之后通知仍然可读（NTF-007）：标题快照还在，accessible 变 false。
//
// 这条同时也是「读路径不再依赖旧列」的证据：被删的正是那件作品，读回来靠的是对象快照。
func TestIntegrationNotificationSurvivesDeletedDemo(t *testing.T) {
	h, _, idSvc := newIdempotencyHandler(t)
	pool := integrationPool(t)

	author, sid := registerVerified(t, pool, idSvc, identity.RegisterInput{
		Email:       "it-notif-tomb@example.com",
		Password:    "correct-horse-battery",
		Username:    "it-notif-tomb",
		DisplayName: "被删作品作者",
	})
	t.Cleanup(func() { cleanupAuthor(t, pool, author.Sub) })
	rater, raterSID := registerVerified(t, pool, idSvc, identity.RegisterInput{
		Email:       "it-notif-tomb-rater@example.com",
		Password:    "correct-horse-battery",
		Username:    "it-notif-tomb-rater",
		DisplayName: "打分人",
	})
	t.Cleanup(func() { cleanupAuthor(t, pool, rater.Sub) })

	slug := submitHTML(t, h, sid, "被删作品", "<h1>gone</h1>")
	publishNow(t, pool, slug)
	if rec := putRating(t, h, slug, raterSID, 5); rec.Code != http.StatusOK {
		t.Fatalf("打分 = %d，想要 200", rec.Code)
	}
	if got := listNotifications(t, h, sid); got.Total != 1 || !got.Items[0].Accessible {
		t.Fatalf("发布态的通知 = %+v，想要 1 条且可访问", got.Items)
	}

	// 作者自己删掉作品：通知不跟着消失，但对象不再可访问。
	body, err := json.Marshal(map[string]string{"reason": "写错了"})
	if err != nil {
		t.Fatalf("序列化删除理由失败: %v", err)
	}
	if rec := lifecycleCall(t, h, http.MethodDelete, slug, sid, body); rec.Code != http.StatusOK {
		t.Fatalf("删除作品 = %d，想要 200（%s）", rec.Code, rec.Body.String())
	}

	got := listNotifications(t, h, sid)
	if got.Total != 1 {
		t.Fatalf("作品删除后通知 = %d 条，想要 1 条（NTF-007）", got.Total)
	}
	item := got.Items[0]
	if item.Accessible {
		t.Fatal("作品已删除，通知不该还说可访问")
	}
	if item.ObjectTitle != "被删作品" || item.ObjectKey != slug {
		t.Fatalf("对象快照 = %q / %q，想要保留删除前的标题与 slug", item.ObjectTitle, item.ObjectKey)
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
