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
// # 为什么给作者与审核视角
//
// 这张表记的是「这件作品经历过什么」：谁在什么时候通过了它、驳回它、发布了它、
// 改过它、回滚过它、下架过它。对作者它是自己的账本；对审核视角它是**审核过程**
// （谁驳回的、理由是什么）——#462 把审核视角接进来，判据是权限键
// （`demo.review` / `demo.publish`），不是角色字符串。
// 对任何访客都不是可读的东西。
//
// # 为什么其他人回 404 而不是 403
//
// 403 会承认「这个 slug 存在，只是你没权限」。对没有权限的人，作品动态 404
// 才与「作品不存在」不可区分。
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
		// 未登录：与「没有权限读」同样处理，不区分（区分了就泄露「你需要登录才能确认
		// 自己是不是作者」，而这不是有用的信息）。
		writeError(w, http.StatusNotFound, api.NotFound, "作品不存在")
		return
	}
	// 作者读自己的记录；审核视角读在审稿件的处理经过（#462）。
	// 其他人仍然 404，不是 403。
	if row.AuthorSub == "" || account.Sub != row.AuthorSub {
		if !h.viewerCanReview(ctx, account.Sub) {
			writeError(w, http.StatusNotFound, api.NotFound, "作品不存在")
			return
		}
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
