package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// 通知域传输层（#437 / NTF-001 – NTF-008）。
//
// 事件类型由 notification_kinds 注册表驱动；这里只负责读/已读，以及给审核与评分
// 两个写入点提供 notifyDemoEvent。通知写失败只记日志：审核与评分本身不能被它拖垮。

const (
	notificationPageSizeDefault = 20
	notificationPageSizeMax     = 100
	notificationRetentionDays   = 90
)

func (h *Handler) notificationRetentionDays(ctx context.Context) int32 {
	raw, err := h.queries.GetSiteSettingNumber(ctx, "notification.retention_days")
	if err != nil {
		return notificationRetentionDays
	}
	days, err := strconv.ParseInt(raw, 10, 32)
	if err != nil || days <= 0 {
		return notificationRetentionDays
	}
	return int32(days)
}

// cleanupNotifications 读时清理过期通知：列表与未读计数都先删再查，
// 过期行就不会被看到，也不需要常驻调度器。
func (h *Handler) cleanupNotifications(ctx context.Context, accountID int64) {
	if _, err := h.queries.DeleteExpiredNotifications(ctx, store.DeleteExpiredNotificationsParams{
		AccountID: accountID,
		Days:      h.notificationRetentionDays(ctx),
	}); err != nil {
		slog.Warn("清理过期通知失败", "account", accountID, "err", err)
	}
}

func notificationView(row store.ListNotificationsRow) api.Notification {
	payload := map[string]interface{}{}
	if len(row.Payload) > 0 {
		if err := json.Unmarshal(row.Payload, &payload); err != nil {
			payload = map[string]interface{}{}
		}
	}
	out := api.Notification{
		Id:         int(row.ID),
		Kind:       row.Kind,
		DemoSlug:   row.DemoSlug,
		DemoTitle:  row.DemoTitle,
		Payload:    payload,
		CreatedAt:  row.CreatedAt.Time,
		Accessible: row.Accessible,
	}
	if row.ReadAt.Valid {
		readAt := row.ReadAt.Time
		out.ReadAt = &readAt
	}
	return out
}

// ListNotifications 对应 GET /api/v1/notifications。
func (h *Handler) ListNotifications(w http.ResponseWriter, r *http.Request, params api.ListNotificationsParams) {
	if h.queries == nil {
		unavailable(w, errors.New("database not configured"))
		return
	}
	account, ctx, cancel, ok := h.requireAccount(w, r)
	if !ok {
		return
	}
	defer cancel()

	page := 1
	if params.Page != nil {
		page = *params.Page
	}
	pageSize := notificationPageSizeDefault
	if params.PageSize != nil {
		pageSize = *params.PageSize
	}
	if page < 1 || pageSize < 1 || pageSize > notificationPageSizeMax {
		writeError(w, http.StatusBadRequest, api.InvalidParam, "page 必须 ≥ 1，pageSize 必须在 1–100")
		return
	}

	h.cleanupNotifications(ctx, account.ID)

	total, err := h.queries.CountNotifications(ctx, account.ID)
	if err != nil {
		unavailable(w, err)
		return
	}
	unread, err := h.queries.CountUnreadNotifications(ctx, account.ID)
	if err != nil {
		unavailable(w, err)
		return
	}
	rows, err := h.queries.ListNotifications(ctx, store.ListNotificationsParams{
		AccountID: account.ID,
		RowLimit:  int32(pageSize),
		RowOffset: int32((page - 1) * pageSize),
	})
	if err != nil {
		unavailable(w, err)
		return
	}

	items := make([]api.Notification, 0, len(rows))
	for _, row := range rows {
		items = append(items, notificationView(row))
	}
	writeJSON(w, http.StatusOK, api.NotificationList{
		Items:       items,
		Total:       int(total),
		UnreadCount: int(unread),
		Page:        page,
		PageSize:    pageSize,
	})
}

// GetUnreadNotificationCount 对应 GET /api/v1/notifications/unread-count。
func (h *Handler) GetUnreadNotificationCount(w http.ResponseWriter, r *http.Request) {
	if h.queries == nil {
		unavailable(w, errors.New("database not configured"))
		return
	}
	account, ctx, cancel, ok := h.requireAccount(w, r)
	if !ok {
		return
	}
	defer cancel()

	h.cleanupNotifications(ctx, account.ID)
	count, err := h.queries.CountUnreadNotifications(ctx, account.ID)
	if err != nil {
		unavailable(w, err)
		return
	}
	writeJSON(w, http.StatusOK, api.UnreadCount{Count: int(count)})
}

// MarkNotificationRead 对应 POST /api/v1/notifications/{id}/read。
func (h *Handler) MarkNotificationRead(w http.ResponseWriter, r *http.Request, id int) {
	if h.queries == nil {
		unavailable(w, errors.New("database not configured"))
		return
	}
	account, ctx, cancel, ok := h.requireAccount(w, r)
	if !ok {
		return
	}
	defer cancel()

	rows, err := h.queries.MarkNotificationRead(ctx, store.MarkNotificationReadParams{
		ID:        int64(id),
		AccountID: account.ID,
	})
	if err != nil {
		unavailable(w, err)
		return
	}
	if rows == 0 {
		// 0 行可能是「已经读过」或「不是自己的 / 不存在」，查一次区分。
		if _, err := h.queries.GetNotificationReadAt(ctx, store.GetNotificationReadAtParams{
			ID:        int64(id),
			AccountID: account.ID,
		}); errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, api.NotFound, "通知不存在")
			return
		} else if err != nil {
			unavailable(w, err)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// MarkAllNotificationsRead 对应 POST /api/v1/notifications/read-all。
func (h *Handler) MarkAllNotificationsRead(w http.ResponseWriter, r *http.Request) {
	if h.queries == nil {
		unavailable(w, errors.New("database not configured"))
		return
	}
	account, ctx, cancel, ok := h.requireAccount(w, r)
	if !ok {
		return
	}
	defer cancel()

	if _, err := h.queries.MarkAllNotificationsRead(ctx, account.ID); err != nil {
		unavailable(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// notifyDemoEvent 给作品作者写一条通知。
//
// 作者注销（account 为空）、操作者就是作者（NTF-004）、写库失败都只是静默跳过/记日志：
// 通知是附加动作，不能反向影响审核或评分。
func (h *Handler) notifyDemoEvent(ctx context.Context, slug, kind string, payload any, actorID int64) {
	if h.queries == nil {
		return
	}
	target, err := h.queries.GetDemoNotifyTarget(ctx, slug)
	if errors.Is(err, pgx.ErrNoRows) {
		return
	}
	if err != nil {
		slog.Warn("查通知接收者失败", "slug", slug, "err", err)
		return
	}
	if !target.AccountID.Valid || target.AccountID.Int64 == actorID {
		return
	}

	data, err := json.Marshal(payload)
	if err != nil {
		slog.Warn("序列化通知 payload 失败", "slug", slug, "kind", kind, "err", err)
		return
	}
	if err := h.queries.InsertNotification(ctx, store.InsertNotificationParams{
		AccountID: target.AccountID.Int64,
		Kind:      kind,
		DemoSlug:  target.Slug,
		DemoTitle: target.Title,
		Payload:   data,
	}); err != nil {
		slog.Warn("写通知失败", "slug", slug, "kind", kind, "err", err)
	}
}
