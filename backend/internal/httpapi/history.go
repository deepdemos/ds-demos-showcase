package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/identity"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// 账号浏览历史（#426 第二半）。
//
// # 为什么写入口挂在浏览计数上而不是独立端点
//
// 详情页已经为统计调了一次 POST /demos/{slug}/view。再让历史页/详情页发第二个
// 「记历史」请求，会多一次往返、多一个可能失败的写，而且两件事天然是同一时刻发生的。
// 所以写路径与计数共用入口，区别只在「有没有登录 + 开关是否打开」。
//
// # 关掉之后为什么不清
//
// HIST-008：关闭只停新增；已有记录要不要删是用户的下一个决定（HIST-009 要求
// 清空前先说明会删掉什么）。把两件事塞进一个开关，用户点「关」时不会预期历史消失。

const (
	historyPageSizeDefault = 20
	historyPageSizeMax     = 100
)

// requireAccount 取当前会话账号；未登录回 401。
//
// 这些端点与 /me 一样是「本人数据」入口，不是公开读——所以身份错误交给
// writeIdentityError 统一映射（401 / 403 / 503）。
func (h *Handler) requireAccount(w http.ResponseWriter, r *http.Request) (identity.Account, context.Context, context.CancelFunc, bool) {
	ctx, cancel := context.WithTimeout(r.Context(), queryTimeout)
	if h.identity == nil {
		cancel()
		writeError(w, http.StatusServiceUnavailable, api.Unavailable, "服务暂时不可用")
		return identity.Account{}, ctx, cancel, false
	}
	account, err := h.identity.Me(ctx, readSessionCookie(r))
	if err != nil {
		h.writeIdentityError(w, err)
		return identity.Account{}, ctx, cancel, false
	}
	return account, ctx, cancel, true
}

// GetMyHistory 对应 GET /api/v1/me/history。
func (h *Handler) GetMyHistory(w http.ResponseWriter, r *http.Request, params api.GetMyHistoryParams) {
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
	pageSize := historyPageSizeDefault
	if params.PageSize != nil {
		pageSize = *params.PageSize
	}
	if page < 1 || pageSize < 1 || pageSize > historyPageSizeMax {
		writeError(w, http.StatusBadRequest, api.InvalidParam, "page 必须 ≥ 1，pageSize 必须在 1..100 之间")
		return
	}

	enabled, err := h.queries.GetUserHistoryEnabled(ctx, account.ID)
	if err != nil {
		unavailable(w, err)
		return
	}
	rows, err := h.queries.ListAccountHistory(ctx, store.ListAccountHistoryParams{
		AccountID: account.ID,
		RowLimit:  int32(pageSize),
		RowOffset: int32((page - 1) * pageSize),
	})
	if err != nil {
		unavailable(w, err)
		return
	}
	total, err := h.queries.CountAccountHistory(ctx, account.ID)
	if err != nil {
		unavailable(w, err)
		return
	}

	items := make([]api.AccountHistoryItem, 0, len(rows))
	for _, row := range rows {
		item := api.AccountHistoryItem{
			Slug:     row.Slug,
			ViewedAt: row.ViewedAt.Time,
		}
		if row.Title.Valid {
			title := row.Title.String
			item.Title = &title
		}
		if row.CoverUrl.Valid {
			cover := row.CoverUrl.String
			item.CoverUrl = &cover
		}
		items = append(items, item)
	}
	writeJSON(w, http.StatusOK, api.AccountHistory{
		Enabled:  enabled,
		Items:    items,
		Total:    int(total),
		Page:     page,
		PageSize: pageSize,
	})
}

// DeleteMyHistory 对应 DELETE /api/v1/me/history。幂等：没有记录也 204。
func (h *Handler) DeleteMyHistory(w http.ResponseWriter, r *http.Request) {
	if h.queries == nil {
		unavailable(w, errors.New("database not configured"))
		return
	}
	account, ctx, cancel, ok := h.requireAccount(w, r)
	if !ok {
		return
	}
	defer cancel()

	if _, err := h.queries.ClearAccountHistory(ctx, account.ID); err != nil {
		unavailable(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// UpdateMyHistorySettings 对应 PUT /api/v1/me/history/settings。
func (h *Handler) UpdateMyHistorySettings(w http.ResponseWriter, r *http.Request) {
	if h.queries == nil {
		unavailable(w, errors.New("database not configured"))
		return
	}
	account, ctx, cancel, ok := h.requireAccount(w, r)
	if !ok {
		return
	}
	defer cancel()

	// 用指针解，才能区分「显式 false」与「根本没传 enabled」。
	var body struct {
		Enabled *bool `json:"enabled"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, api.InvalidParam, "请求体不是合法的 JSON")
		return
	}
	if body.Enabled == nil {
		writeError(w, http.StatusBadRequest, api.InvalidParam, "缺少 enabled")
		return
	}
	if err := h.queries.SetUserHistoryEnabled(ctx, store.SetUserHistoryEnabledParams{
		AccountID: account.ID,
		Enabled:   *body.Enabled,
	}); err != nil {
		unavailable(w, err)
		return
	}
	writeJSON(w, http.StatusOK, api.AccountHistorySettings{Enabled: *body.Enabled})
}

// recordAccountView 在浏览计数之后顺带写账号历史。
//
// 任何一步失败都不影响响应：历史是**附加信息**，记录失败不该让一次浏览报错。
// 顺序是「先查开关、再写、最后裁剪」——裁剪失败最多多留几条，不会丢已记的浏览。
func (h *Handler) recordAccountView(ctx context.Context, accountID int64, slug string) {
	enabled, err := h.queries.GetUserHistoryEnabled(ctx, accountID)
	if err != nil {
		slog.Warn("读账号历史开关失败", "account", accountID, "err", err)
		return
	}
	if !enabled {
		return
	}
	if err := h.queries.UpsertAccountViewHistory(ctx, store.UpsertAccountViewHistoryParams{
		AccountID: accountID,
		Slug:      slug,
	}); err != nil {
		slog.Warn("记账号历史失败", "account", accountID, "slug", slug, "err", err)
		return
	}
	limit, err := h.queries.GetAccountHistoryLimit(ctx)
	if err != nil {
		slog.Warn("读账号历史上限失败", "err", err)
		return
	}
	if err := h.queries.TrimAccountViewHistory(ctx, store.TrimAccountViewHistoryParams{
		AccountID: accountID,
		Keep:      limit,
	}); err != nil {
		slog.Warn("裁剪账号历史失败", "account", accountID, "err", err)
	}
}
