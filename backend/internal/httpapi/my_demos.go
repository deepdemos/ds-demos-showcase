package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// ListMyDemos 对应 GET /api/v1/me/demos。
//
// 这是作者唯一能看到自己「草稿 / 待审 / 被驳回 / 已下架 / 已软删」的地方：
// 公开列表只含已上架作品，审核队列只列 pending 且仅审核员可见。
// 少一个这样的出口，作者被驳回一次就只能靠记住 URL 回来。
func (h *Handler) ListMyDemos(w http.ResponseWriter, r *http.Request, params api.ListMyDemosParams) {
	if h.queries == nil || h.identity == nil {
		unavailable(w, errors.New("database not configured"))
		return
	}
	page, pageSize, ok := paginate(w, params.Page, params.PageSize)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), queryTimeout)
	defer cancel()
	account, err := h.identity.Me(ctx, readSessionCookie(r))
	if err != nil {
		h.writeIdentityError(w, err)
		return
	}

	total, err := h.queries.CountMyDemos(ctx, account.UserID)
	if err != nil {
		unavailable(w, err)
		return
	}
	rows, err := h.queries.ListMyDemos(ctx, store.ListMyDemosParams{
		AuthorID:  account.UserID,
		RowOffset: int32((page - 1) * pageSize),
		RowLimit:  int32(pageSize),
	})
	if err != nil {
		unavailable(w, err)
		return
	}

	// 空列表要是 [] 不是 null：前端直接 .map，null 会炸。
	items := make([]api.MyDemoSummary, 0, len(rows))
	for _, row := range rows {
		items = append(items, api.MyDemoSummary{
			Slug:            row.Slug,
			Title:           textPtr(row.Title),
			CoverUrl:        textPtr(row.CoverUrl),
			CreatedAt:       row.CreatedAt.Time,
			UpdatedAt:       row.UpdatedAt.Time,
			LifecycleState:  api.LifecycleState(row.LifecycleState),
			ModerationState: api.MyDemoSummaryModerationState(row.ModerationState),
			DeletedAt:       timePtrOf(row.DeletedAt),
			PurgeAfter:      timePtrOf(row.PurgeAfter),
		})
	}
	writeJSON(w, http.StatusOK, api.MyDemoList{
		Items:    items,
		Page:     page,
		PageSize: pageSize,
		Total:    int(total),
	})
}
