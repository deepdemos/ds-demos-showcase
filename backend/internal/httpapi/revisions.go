package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/demo"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/identity"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// 版本历史与回滚的传输层。
//
// # 为什么列表不是公开端点
//
// 版本列表里有「哪些版本还没过审」这类只有作者和审核员该知道的信息。
// 公开作品页不需要它——访客看到的永远是 published_revision_id 指向的那一版。

// ListDemoRevisions 处理 `GET /api/v1/demos/{slug}/revisions`。
func (h *Handler) ListDemoRevisions(w http.ResponseWriter, r *http.Request, slug string) {
	if h.identity == nil || h.queries == nil || h.demos == nil {
		writeError(w, http.StatusServiceUnavailable, api.Unavailable, "服务暂不可用")
		return
	}
	account, err := h.identity.Me(r.Context(), readSessionCookie(r))
	if err != nil {
		writeError(w, http.StatusUnauthorized, api.Unauthorized, "请先登录")
		return
	}

	if !h.maySeeRevisions(r.Context(), slug, account) {
		// 与编辑同口径：不承认 slug 存在。审核员看得到别人的，作者看得到自己的。
		writeError(w, http.StatusNotFound, api.NotFound, "作品不存在")
		return
	}

	revisions, err := h.demos.ListRevisions(r.Context(), slug)
	if err != nil {
		h.writeDemoError(w, err)
		return
	}

	items := make([]api.DemoRevision, 0, len(revisions))
	for _, rev := range revisions {
		items = append(items, api.DemoRevision{
			RevNo:       rev.RevNo,
			Title:       rev.Title,
			DemoType:    rev.DemoType,
			Source:      api.DemoRevisionSource(rev.Source),
			FileCount:   int(rev.FileCount),
			TotalBytes:  rev.TotalBytes,
			CreatedAt:   rev.CreatedAt,
			Deleted:     rev.Deleted,
			Erased:      rev.Erased,
			IsCurrent:   rev.IsCurrent,
			IsPublished: rev.IsPublished,
		})
	}
	writeJSON(w, http.StatusOK, api.RevisionList{Items: items})
}

// RestoreDemoRevision 处理 `POST /api/v1/demos/{slug}/revisions/{revNo}/restore`。
func (h *Handler) RestoreDemoRevision(w http.ResponseWriter, r *http.Request, slug string, revNo int) {
	if h.identity == nil || h.demos == nil {
		writeError(w, http.StatusServiceUnavailable, api.Unavailable, "服务暂不可用")
		return
	}
	account, err := h.identity.Me(r.Context(), readSessionCookie(r))
	if err != nil {
		writeError(w, http.StatusUnauthorized, api.Unauthorized, "请先登录")
		return
	}

	res, err := h.demos.Restore(r.Context(), slug, revNo, account.UserID)
	if err != nil {
		h.writeDemoError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, api.RestoreResult{
		Slug:              res.Slug,
		RestoredFromRevNo: res.RestoredFromNo,
		RevisionNo:        res.NewRevNo,
		ModerationState:   api.RestoreResultModerationState(res.ModerationState),
	})
}

// maySeeRevisions 判断这个人能不能看这件作品的版本列表：作者本人，或持 `demo.review`。
//
// 与 preview 的 mayPreviewDraft 是同一件事的两种表达，但不合并：
// 那边传进来的是 author_sub（它已经查到了行），这边只有 slug，得先查作品归属。
// 合并的写法会变成「先查一遍归属再判」，两边各自调用一次反而更直白。
func (h *Handler) maySeeRevisions(ctx context.Context, slug string, account identity.Account) bool {
	row, err := h.queries.GetDemoBySlug(ctx, slug)
	if err != nil {
		return false
	}
	if row.AuthorSub != "" && row.AuthorSub == account.Sub {
		return true
	}
	ok, err := h.queries.HasPermission(ctx, store.HasPermissionParams{
		Sub:           account.Sub,
		PermissionKey: "demo.review",
	})
	if err != nil {
		return false
	}
	return ok
}

// errRevisionNoInvalid 是路径参数解析失败时的哨兵，不进领域包。
var errRevisionNoInvalid = errors.New("httpapi: 版本号不是正整数")

// parseRevNo 解析路径里的版本号。chi 的 `{revNo}` 只会匹配到下一个 `/` 之前，
// 所以 `abc` 会原样进来，必须自己挡——不挡的话 strconv 的 0 值会让它去查「第 0 版」。
func parseRevNo(raw string) (int, error) {
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return 0, errRevisionNoInvalid
	}
	return n, nil
}

// revNoParam 从 chi 的路由上下文里取版本号，解析失败直接写 400。
func revNoParam(w http.ResponseWriter, r *http.Request) (int, bool) {
	n, err := parseRevNo(chi.URLParam(r, "revNo"))
	if err != nil {
		writeError(w, http.StatusBadRequest, api.InvalidParam, "版本号必须是从 1 开始的正整数")
		return 0, false
	}
	return n, true
}

// 编译期确认接口签名与生成物一致：契约一改这里就会红，而不是等到路由注册时才崩。
var _ = demo.ErrRevisionNotFound
