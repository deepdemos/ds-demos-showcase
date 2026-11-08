package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// 「为你推荐」端点：`GET /api/v1/recommendations`。
//
// # 为什么要登录
//
// 匿名访客的去重键是 `HMAC(ip)`。拿它做个性化，等于把同一个 NAT 后面的所有人
// 当成一个人——早上别人看过的作品，下午会出现在你的「为你推荐」里。那比没有推荐更糟。
//
// # 为什么每次现算
//
// 画像表一旦落库，就要回答「什么时候过期」「删作品时怎么清」，两件事的成本都高于
// 这条查询本身（候选集是全部已上架作品，上限十二条）。
//
// # 为什么排除自己看过 / 打过分的
//
// 推荐里出现刚看完的那件，读者会以为系统没在听。这一条写在 SQL 里而不是 Go 里：
// 排除之后再取前 N 条，与取前 N 条再过滤，结果不是一回事。
const (
	recommendationDefaultLimit = 6
	recommendationMaxLimit     = 12
	recommendationTimeout      = 5 * time.Second
)

// ListRecommendations 处理 `GET /api/v1/recommendations`。
func (h *Handler) ListRecommendations(w http.ResponseWriter, r *http.Request, params api.ListRecommendationsParams) {
	if h.demos == nil || h.identity == nil {
		writeError(w, http.StatusServiceUnavailable, api.Unavailable, "服务暂时不可用")
		return
	}

	limit := recommendationDefaultLimit
	if params.Limit != nil {
		limit = *params.Limit
	}
	if limit < 1 || limit > recommendationMaxLimit {
		writeError(w, http.StatusBadRequest, api.InvalidParam, "limit 超出范围（1 到 12）")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), recommendationTimeout)
	defer cancel()

	account, err := h.identity.Me(ctx, readSessionCookie(r))
	if err != nil {
		// 401 而不是空数组：空数组是「还不知道推荐什么」，这里是「没登录」，
		// 两者在界面上的处置不一样（前者整块不渲染，后者可以引导登录）。
		writeError(w, http.StatusUnauthorized, api.Unauthorized, "请先登录后查看推荐")
		return
	}

	viewerKey := h.identity.ViewerKey(account.ID, clientIP(r))
	rows, err := h.queries.ListRecommendedDemos(ctx, store.ListRecommendedDemosParams{
		ViewerKey: viewerKey,
		AuthorID:  account.UserID,
		RowLimit:  int32(limit),
	})
	if err != nil {
		slog.Error("读推荐失败", "viewer", viewerKey, "err", err)
		writeError(w, http.StatusServiceUnavailable, api.Unavailable, "服务暂时不可用")
		return
	}

	// 得分 0 的丢掉：与相关作品同一口径，空着比塞凑数的诚实。
	kept := make([]store.ListRecommendedDemosRow, 0, len(rows))
	ids := make([]int64, 0, len(rows))
	for _, row := range rows {
		if row.Score <= 0 {
			continue
		}
		kept = append(kept, row)
		ids = append(ids, row.ID)
	}
	classes := h.classificationsByIDs(ctx, ids)

	items := make([]api.DemoSummary, 0, len(kept))
	for _, row := range kept {
		items = append(items, api.DemoSummary{
			Slug:            row.Slug,
			Title:           strPtrOrNil(row.Title),
			MatchedIn:       api.DemoSummaryMatchedInTitle,
			CoverUrl:        textPtr(row.CoverUrl),
			CreatedAt:       row.CreatedAt.Time,
			LifecycleState:  api.LifecycleStatePublished,
			Classifications: classes[row.ID],
		})
	}

	writeJSON(w, http.StatusOK, api.RecommendationList{Items: items})
}
