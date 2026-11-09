package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

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

	// 三个筛选都是可空的原始维度（生命周期 / 审核态 / 是否软删），不在这里折成
	// 「状态桶」——桶是界面的说法，会随界面变；接口给的是能自由组合的那三维。
	lifecycle, ok := optionalEnum(w, "lifecycle", enumText(params.Lifecycle),
		[]string{"draft", "published", "archived", "removed"})
	if !ok {
		return
	}
	moderation, ok := optionalEnum(w, "moderation", enumText(params.Moderation),
		[]string{"pending", "approved", "rejected"})
	if !ok {
		return
	}
	deleted := optionalBool(params.Deleted)

	total, err := h.queries.CountMyDemos(ctx, store.CountMyDemosParams{
		AuthorID:   account.UserID,
		Lifecycle:  lifecycle,
		Moderation: moderation,
		Deleted:    deleted,
	})
	if err != nil {
		unavailable(w, err)
		return
	}
	rows, err := h.queries.ListMyDemos(ctx, store.ListMyDemosParams{
		AuthorID:   account.UserID,
		Lifecycle:  lifecycle,
		Moderation: moderation,
		Deleted:    deleted,
		RowOffset:  int32((page - 1) * pageSize),
		RowLimit:   int32(pageSize),
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

// enumText 把生成的枚举指针转成字符串指针：校验逻辑只关心「有没有值、值合不合法」。
func enumText[T ~string](v *T) *string {
	if v == nil {
		return nil
	}
	s := string(*v)
	return &s
}

// optionalEnum 把一个可空枚举参数转成 pgtype.Text。
//
// 取值校验放在这里而不是靠 SQL 兜底：拼错的 `lifecycle=draf` 如果不报错，
// 会安静地返回空列表，作者会以为自己的作品没了。**空列表与筛选条件不匹配
// 必须是两件事。**
func optionalEnum(w http.ResponseWriter, name string, raw *string, allowed []string) (pgtype.Text, bool) {
	if raw == nil || *raw == "" {
		return pgtype.Text{}, true
	}
	for _, v := range allowed {
		if *raw == v {
			return pgtype.Text{String: *raw, Valid: true}, true
		}
	}
	writeError(w, http.StatusBadRequest, api.InvalidParam,
		"筛选条件 "+name+" 取值非法："+*raw+"（可选 "+strings.Join(allowed, " / ")+"）")
	return pgtype.Text{}, false
}

// optionalBool 同理：指针为空 = 不过滤。
func optionalBool(raw *bool) pgtype.Bool {
	if raw == nil {
		return pgtype.Bool{}
	}
	return pgtype.Bool{Bool: *raw, Valid: true}
}
