package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

const (
	relatedDefaultLimit = 6
	relatedMaxLimit     = 12
)

// ListRelatedDemos 相关作品（内容信号：共用分类 / 共用模型 / 同一作者）。
//
// 为什么不在 Go 里逐件算：候选集是「全部已上架作品」，逐件查关联就是 N+1。
// 一条语句把 score 算出来，只在需要时才取分类做卡片上的小标签。
func (h *Handler) ListRelatedDemos(w http.ResponseWriter, r *http.Request, slug string, params api.ListRelatedDemosParams) {
	if h.queries == nil {
		unavailable(w, errors.New("database not configured"))
		return
	}

	limit := relatedDefaultLimit
	if params.Limit != nil {
		limit = *params.Limit
	}
	if limit < 1 || limit > relatedMaxLimit {
		writeError(w, http.StatusBadRequest, api.InvalidParam, "limit 超出范围（1 到 12）")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), queryTimeout)
	defer cancel()

	source, err := h.queries.DemoIDForRelated(ctx, slug)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// 不区分「不存在」与「不可公开访问」：403 会承认这个 slug 存在。
			writeError(w, http.StatusNotFound, api.NotFound, "作品不存在")
			return
		}
		unavailable(w, err)
		return
	}

	rows, err := h.queries.ListRelatedDemos(ctx, store.ListRelatedDemosParams{
		SourceID:         source.ID,
		SourceRevisionID: source.CurrentRevisionID,
		SourceAuthorID:   source.AuthorID.Int64,
		RowLimit:         int32(limit),
	})
	if err != nil {
		unavailable(w, err)
		return
	}

	// 得分 0 的作品也查得出来（SQL 按 score 排序但不过滤），这里丢掉：
	// 契约承诺「没有相关作品时返回空数组」，而不是「返回一批无关的凑数」。
	kept := make([]store.ListRelatedDemosRow, 0, len(rows))
	for _, row := range rows {
		if row.Score > 0 {
			kept = append(kept, row)
		}
	}

	// 分类标签要按 demo_id 取，而榜单那套列集里没有 id。
	// 逐条补 id 并把 (row, id) 成对保存——用两个切片按下标对齐，
	// 一旦中间有一条失败就会整体错位，那种 bug 只在「某件作品刚好被删」时才出现。
	type linked struct {
		row store.ListRelatedDemosRow
		id  int64
	}
	linkedRows := make([]linked, 0, len(kept))
	ids := make([]int64, 0, len(kept))
	for _, row := range kept {
		id, err := h.demoIDBySlug(ctx, row.Slug)
		if err != nil {
			slog.Warn("相关作品取 id 失败", "slug", row.Slug, "err", err)
			continue
		}
		linkedRows = append(linkedRows, linked{row: row, id: id})
		ids = append(ids, id)
	}
	classes := h.classificationsByIDs(ctx, ids)

	items := make([]api.DemoSummary, 0, len(linkedRows))
	for _, item := range linkedRows {
		items = append(items, api.DemoSummary{
			Slug:            item.row.Slug,
			Title:           strPtrOrNil(item.row.Title),
			MatchedIn:       api.DemoSummaryMatchedInTitle,
			CoverUrl:        textPtr(item.row.CoverUrl),
			CreatedAt:       item.row.CreatedAt.Time,
			LifecycleState:  api.LifecycleStatePublished,
			Classifications: classes[item.id],
		})
	}

	writeJSON(w, http.StatusOK, api.RelatedList{Items: items})
}

// demoIDBySlug 只用来给卡片补分类标签。
//
// 单列为一次查询是因为 ListRelatedDemos 的列集是「榜单卡片」那一套（slug / 标题 /
// 封面 / 时间 / 分数），没有 id。把 id 加进那个查询会给一个只读端点多加一列
// 仅供内部使用的数据；而在结果集只有 6 条的情况下多查一次是廉价的。
func (h *Handler) demoIDBySlug(ctx context.Context, slug string) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	row, err := h.queries.DemoIDForRelated(ctx, slug)
	if err != nil {
		return 0, err
	}
	return row.ID, nil
}
