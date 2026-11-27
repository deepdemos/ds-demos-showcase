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
// 事件类型由 notification_kinds 注册表驱动；这里只负责读/已读，以及给各个写入点
// 提供写入路径。通知写失败只记日志：审核、评分、裁决本身不能被它拖垮。
//
// 通知对象是「类型 + 键 + 标题快照」（#591 从「作品」泛化而来）：作品、模型、提案都能挂，
// 存快照不存外键，所以对象删了通知还在（NTF-007）。

const (
	notificationPageSizeDefault = 20
	notificationPageSizeMax     = 100
	notificationRetentionDays   = 90
)

// objectTypeDemo 是作品对象的类型键。只有它走公开可见性判据（见 ListNotifications 的 SQL）；
// 其它类型（model / proposal）不套作品可见性。
const objectTypeDemo = "demo"

// notificationObject 是通知指向的对象：类型 + 键 + 标题快照。
//
// 键就是「拿它能不能找到那个对象」：作品与模型是 slug，提案是提案 id 的十进制串。
type notificationObject struct {
	Type  string
	Key   string
	Title string
}

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
		Id:          int(row.ID),
		Kind:        row.Kind,
		ObjectType:  row.ObjectType,
		ObjectKey:   row.ObjectKey,
		ObjectTitle: row.ObjectTitle,
		Payload:     payload,
		CreatedAt:   row.CreatedAt.Time,
		Accessible:  row.Accessible,
	}
	// 契约里的 demoSlug / demoTitle 是给老调用方的展示缓存：作品类事件把对象快照摊到这两个
	// 字段上，老调用方读到的值与旧列时代逐字一致；非作品对象给空串（它们没有作品可指）。
	// 旧列已由迁移 0042 删除，这里只是**派生**，不是第二份事实源。
	if row.ObjectType == objectTypeDemo {
		out.DemoSlug = row.ObjectKey
		out.DemoTitle = row.ObjectTitle
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
	h.notifyObject(ctx, target.AccountID.Int64, kind, notificationObject{
		Type:  objectTypeDemo,
		Key:   target.Slug,
		Title: target.Title,
	}, payload)
}

// notifyObject 给指定账号写一条对象通知。接收者由调用方解析——模型 / 提案这类事件
// 没有「作品作者」可查，谁该收到是各域自己的事（#595 的驳回通知就这么走）。
//
// 对象只写 object_type / object_key / object_title 三列（旧的作品列已由迁移 0042 删除）：
// 作品、模型、提案走同一条写入路径，差别只在传进来的类型与键。
//
// 写库失败只记日志：通知是附加动作，不能反向影响裁决。
func (h *Handler) notifyObject(ctx context.Context, accountID int64, kind string, object notificationObject, payload any) {
	if h.queries == nil {
		return
	}
	data, err := json.Marshal(payload)
	if err != nil {
		slog.Warn("序列化通知 payload 失败", "object", object.Type+":"+object.Key, "kind", kind, "err", err)
		return
	}
	params := store.InsertNotificationParams{
		AccountID:   accountID,
		Kind:        kind,
		ObjectType:  object.Type,
		ObjectKey:   object.Key,
		ObjectTitle: object.Title,
		Payload:     data,
	}
	if err := h.queries.InsertNotification(ctx, params); err != nil {
		slog.Warn("写通知失败", "object", object.Type+":"+object.Key, "kind", kind, "err", err)
	}
}
