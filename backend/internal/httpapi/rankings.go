package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

const (
	rankingDefaultLimit = 20
	rankingMaxLimit     = 100
)

// rankingWindows 把 range 映射成「往前多少时间」，键是只在 week / month 时存在的窗口。
// all 不在此表里，对应不传 created_after（NULL）。
//
// 时间锚点是 demos.created_at（SOC-016 原文），不是投票时间——所以老作品今天被投票
// 也进不了近 7 天榜。这一点在契约与需求文档里都写死了，别在这里「顺手改成投票时间」。
var rankingWindows = map[api.ListRankingsParamsRange]time.Duration{
	api.ListRankingsParamsRangeWeek:  7 * 24 * time.Hour,
	api.ListRankingsParamsRangeMonth: 30 * 24 * time.Hour,
}

// rankingBoards 是六个口径的合法取值。写成集合而不是 switch，是因为
// 「校验」与「文档」都从同一处读，少一份会漂的副本。
var rankingBoards = map[string]bool{
	"avg": true, "god": true, "ghost": true, "net": true, "count": true, "heat": true,
}

// ListRankings 作品榜（SOC-013 六口径 × SOC-016 三档时间范围）。
//
// 为什么六个榜共用一个端点而不是六条路径：它们返回的是同一种东西（名次 + 作品卡片），
// 差异只有一个排序键与「是否排除零评分」。六个路径会让前端的六次换榜变成六种响应形状，
// 而分页、时间范围、空态处理全都要各写一遍。
func (h *Handler) ListRankings(w http.ResponseWriter, r *http.Request, params api.ListRankingsParams) {
	if h.queries == nil {
		unavailable(w, errors.New("database not configured"))
		return
	}

	board := "avg"
	if params.Board != nil {
		board = string(*params.Board)
	}
	if !rankingBoards[board] {
		writeError(w, http.StatusBadRequest, api.InvalidParam, "榜单口径取值非法（avg / god / ghost / net / count / heat）")
		return
	}

	rangeName := api.ListRankingsParamsRangeAll
	if params.Range != nil {
		rangeName = *params.Range
	}
	if rangeName != api.ListRankingsParamsRangeAll &&
		rangeName != api.ListRankingsParamsRangeWeek &&
		rangeName != api.ListRankingsParamsRangeMonth {
		writeError(w, http.StatusBadRequest, api.InvalidParam, "时间范围取值非法（all / week / month）")
		return
	}

	limit := rankingDefaultLimit
	if params.Limit != nil {
		limit = *params.Limit
	}
	if limit < 1 || limit > rankingMaxLimit {
		writeError(w, http.StatusBadRequest, api.InvalidParam, "每页条数超出范围（1 到 100）")
		return
	}
	offset := 0
	if params.Offset != nil {
		offset = *params.Offset
	}
	if offset < 0 {
		writeError(w, http.StatusBadRequest, api.InvalidParam, "offset 不能为负")
		return
	}

	var createdAfter pgtype.Timestamptz
	if window, ok := rankingWindows[rangeName]; ok {
		createdAfter = pgtype.Timestamptz{Time: time.Now().Add(-window), Valid: true}
	}

	ctx, cancel := context.WithTimeout(r.Context(), queryTimeout)
	defer cancel()

	total, err := h.queries.CountRankings(ctx, store.CountRankingsParams{
		Board:        board,
		CreatedAfter: createdAfter,
	})
	if err != nil {
		unavailable(w, err)
		return
	}

	rows, err := h.queries.ListRankings(ctx, store.ListRankingsParams{
		Board:        board,
		CreatedAfter: createdAfter,
		RowOffset:    int32(offset),
		RowLimit:     int32(limit),
	})
	if err != nil {
		unavailable(w, err)
		return
	}

	ids := make([]int64, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	classes := h.classificationsByIDs(ctx, ids)

	items := make([]api.RankingEntry, 0, len(rows))
	for i, row := range rows {
		// 名次是连续编号（不是 SQL 的 row_number）：分页的第二页要从 offset+1 接着数，
		// 而「第 21 名」这种链接必须总能翻到东西——见契约里 RankingEntry.rank 的说明。
		items = append(items, api.RankingEntry{
			Rank:  offset + i + 1,
			Value: row.Value,
			Demo: api.DemoSummary{
				Slug: row.Slug,
				// 榜单不查墓碑（视图只出已上架），所以标题一定非空；
				// 契约里 title 可空是因为墓碑，这里用指针包一层保持形状一致。
				Title: strPtrOrNil(row.Title),
				// 榜单没有搜索词，所以 matchedIn 恒为 title（契约里也是这么写的）。
				MatchedIn:       api.DemoSummaryMatchedInTitle,
				CoverUrl:        textPtr(row.CoverUrl),
				CreatedAt:       row.CreatedAt.Time,
				LifecycleState:  api.LifecycleStatePublished,
				Classifications: classes[row.ID],
			},
		})
	}

	writeJSON(w, http.StatusOK, api.RankingList{
		Board: api.RankingListBoard(board),
		Range: api.RankingListRange(rangeName),
		Items: items,
		Total: int(total),
	})
}

// classificationsByIDs 一次取整页作品的分类。
//
// 与 classificationsFor 的区别只是输入形状（榜单的行类型与列表的行类型是 sqlc 生成的
// 两个结构体）。读失败只记日志并返回空 map：分类是卡片上的装饰性文字，
// 不该因为它把榜单打成 503。
func (h *Handler) classificationsByIDs(ctx context.Context, ids []int64) map[int64][]api.ClassificationRef {
	out := make(map[int64][]api.ClassificationRef, len(ids))
	if len(ids) == 0 {
		return out
	}

	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	cls, err := h.queries.ListClassificationsForDemos(ctx, ids)
	if err != nil {
		slog.Error("读榜单作品的分类失败", "count", len(ids), "err", err)
		return out
	}
	for _, c := range cls {
		out[c.DemoID] = append(out[c.DemoID], api.ClassificationRef{
			Dimension: api.ClassificationRefDimension(c.Dimension),
			Slug:      c.Slug,
			Label:     c.Label,
		})
	}
	return out
}
