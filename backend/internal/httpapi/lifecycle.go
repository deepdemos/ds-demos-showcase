package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/demo"
)

// 作品生命周期：下架 / 重新上架 / 软删 / 恢复。
//
// 四个动作分成两条轴，别混：
//   - 下架 / 上架改的是可见性层（`lifecycle_state`），下架后作品对外是墓碑页；
//   - 删除 / 恢复改的是删除标记层（`deleted_at`），删除后作品对外根本不存在。
//
// 恢复窗口在删除那一刻定型（`purge_after`），所以删除的响应里带 `purgeAfter`——
// 前端要能直接告诉作者「30 天内可恢复」，而不是让他去猜。

// ArchiveDemo 下架一件作品（作者本人）。
func (h *Handler) ArchiveDemo(w http.ResponseWriter, r *http.Request, slug string) {
	in, ok := h.lifecycleInput(w, r, slug)
	if !ok {
		return
	}
	result, err := h.demos.Archive(r.Context(), in)
	if err != nil {
		h.writeDemoError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, api.DemoLifecycleResult{
		Slug:           result.Slug,
		LifecycleState: api.LifecycleState(result.LifecycleState),
	})
}

// UnarchiveDemo 重新上架（作者本人）。
func (h *Handler) UnarchiveDemo(w http.ResponseWriter, r *http.Request, slug string) {
	in, ok := h.lifecycleInput(w, r, slug)
	if !ok {
		return
	}
	result, err := h.demos.Unarchive(r.Context(), in)
	if err != nil {
		h.writeDemoError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, api.DemoLifecycleResult{
		Slug:           result.Slug,
		LifecycleState: api.LifecycleState(result.LifecycleState),
	})
}

// DeleteDemo 软删一件作品（作者本人）。理由可选，走 JSON 体。
func (h *Handler) DeleteDemo(w http.ResponseWriter, r *http.Request, slug string) {
	in, ok := h.lifecycleInput(w, r, slug)
	if !ok {
		return
	}
	// DELETE 带体是允许的，但不是所有代理都转；所以体为空也照常工作。
	if r.ContentLength > 0 {
		var body api.DemoDeleteRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, api.InvalidParam, "请求体不是合法 JSON")
			return
		}
		if body.Reason != nil {
			in.Reason = *body.Reason
		}
	}
	result, err := h.demos.SoftDelete(r.Context(), in)
	if err != nil {
		h.writeDemoError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, api.DemoDeleteResult{
		Slug:           result.Slug,
		LifecycleState: api.LifecycleState(result.LifecycleState),
		PurgeAfter:     result.PurgeAfter,
	})
}

// RestoreDemo 恢复一件软删的作品（作者本人）。
//
// 路径与「回滚到某个历史版本」的 `/revisions/{revNo}/restore` 分开写：
// 那一个恢复的是**内容版本**，这一个恢复的是**被删掉的作品**。同名会让调用方猜。
func (h *Handler) RestoreDemo(w http.ResponseWriter, r *http.Request, slug string) {
	in, ok := h.lifecycleInput(w, r, slug)
	if !ok {
		return
	}
	result, err := h.demos.RestoreDeleted(r.Context(), in)
	if err != nil {
		h.writeDemoError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, api.DemoLifecycleResult{
		Slug:           result.Slug,
		LifecycleState: api.LifecycleState(result.LifecycleState),
	})
}

// lifecycleInput 认人并组装入参。返回 false 表示已经写过响应。
//
// 四个端点共用：认人、服务未接线的判定、ActorID 的口径（users.id）只写一次。
func (h *Handler) lifecycleInput(w http.ResponseWriter, r *http.Request, slug string) (demo.LifecycleInput, bool) {
	if h.demos == nil || h.identity == nil {
		writeError(w, http.StatusServiceUnavailable, api.Unavailable, "服务暂时不可用")
		return demo.LifecycleInput{}, false
	}
	account, err := h.identity.Me(r.Context(), readSessionCookie(r))
	if err != nil {
		h.writeIdentityError(w, err)
		return demo.LifecycleInput{}, false
	}
	return demo.LifecycleInput{ActorID: account.UserID, Slug: slug}, true
}
