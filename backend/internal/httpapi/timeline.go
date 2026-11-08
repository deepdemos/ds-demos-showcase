package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
)

// 作品动态（`demo_timeline` 的读路径）。
//
// # 为什么只给作者
//
// 这张表记的是「这件作品经历过什么」：谁在什么时候通过了它、驳回它、发布了它、
// 改过它、回滚过它、下架过它。对作者它是自己的账本；对别人它是**审核过程的内幕**
// （谁驳回的、什么时候驳回的），而这些只与作者和审核员有关，不该被任何访客看到。
//
// 审核员也不需要这个端点：他们看待审队列时的依据是内容与理由（`review_reason`），
// 而不是别人的历史动作。多一个出口就多一处要维护的可见性判定。
//
// # 为什么非作者回 404 而不是 403
//
// 403 会承认「这个 slug 存在，只是你没权限」。能被作者以外的人看到的详情本来就只有
// 对外那一版，对这些人的作品动态 404 才与「作品不存在」不可区分。
func (h *Handler) ListDemoTimeline(w http.ResponseWriter, r *http.Request, slug string) {
	if h.queries == nil || h.identity == nil {
		unavailable(w, errors.New("database not configured"))
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), queryTimeout)
	defer cancel()

	row, err := h.queries.GetDemoForTimeline(ctx, slug)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, api.NotFound, "作品不存在")
			return
		}
		unavailable(w, err)
		return
	}
	// 被擦除的作品对包括作者在内的所有人都不存在（与详情、预览同一口径）。
	if row.ErasedAt.Valid {
		writeError(w, http.StatusNotFound, api.NotFound, "作品不存在")
		return
	}

	account, err := h.identity.Me(ctx, readSessionCookie(r))
	if err != nil {
		// 未登录：与「不是作者」同样处理，不区分（区分了就泄露「你需要登录才能确认
		// 自己是不是作者」，而这不是有用的信息）。
		writeError(w, http.StatusNotFound, api.NotFound, "作品不存在")
		return
	}
	if row.AuthorSub == "" || account.Sub != row.AuthorSub {
		writeError(w, http.StatusNotFound, api.NotFound, "作品不存在")
		return
	}

	rows, err := h.queries.ListDemoTimeline(ctx, row.ID)
	if err != nil {
		unavailable(w, err)
		return
	}

	items := make([]api.DemoTimelineEntry, 0, len(rows))
	for _, t := range rows {
		entry := api.DemoTimelineEntry{
			Kind: t.Kind,
			At:   t.CreatedAt.Time,
		}
		if t.Message != "" {
			msg := t.Message
			entry.Message = &msg
		}
		if t.ActorName != "" {
			name := t.ActorName
			entry.ActorName = &name
		}
		items = append(items, entry)
	}
	writeJSON(w, http.StatusOK, api.DemoTimeline{Items: items})
}
