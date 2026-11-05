package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/demo"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/identity"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// 审核裁决的传输层。
//
// # 权限只在这里判一次
//
// `demo.review` 是唯一一道门。它不在 SQL 里判（数据库不知道该是谁在调用），
// 也不在领域层判（领域层拿不到请求），只能在这里——所以这里的 `reviewer`
// 是审核链路唯一的鉴权点，改动它的后果是全部审核端点。
//
// # 为什么未登录回 401、无权限回 403
//
// 401 是「你还没说你是谁」，403 是「我知道你是谁，但你不行」。
// 混成一个值会让前端的处理逻辑变成猜谜：401 该跳登录页，403 该提示无权，
// 而跳登录页之后仍然没权限，用户会在两个页面之间来回弹。
func (h *Handler) reviewer(ctx context.Context, r *http.Request) (demo.Actor, error) {
	if h.identity == nil || h.queries == nil {
		return demo.Actor{}, errNotWired
	}
	account, err := h.identity.Me(ctx, readSessionCookie(r))
	if err != nil {
		return demo.Actor{}, err
	}
	ok, err := h.queries.HasPermission(ctx, store.HasPermissionParams{
		Sub:           account.Sub,
		PermissionKey: "demo.review",
	})
	if err != nil {
		return demo.Actor{}, err
	}
	if !ok {
		return demo.Actor{}, errForbidden
	}
	return demo.Actor{
		AccountID: account.ID,
		UserID:    account.UserID,
		Role:      account.Role,
	}, nil
}

// errForbidden 与 errNotWired 是传输层自己的哨兵，不进领域包——
// 领域层不需要知道「HTTP 状态码」这件事，也不需要知道「依赖没接线」。
var (
	errForbidden = errors.New("httpapi: 没有权限")
	errNotWired  = errors.New("httpapi: 依赖未接线")
)

// writeReviewError 把审核链路上的错误映射成状态码。
func (h *Handler) writeReviewError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, identity.ErrUnauthorized):
		writeError(w, http.StatusUnauthorized, api.Unauthorized, "请先登录")
	case errors.Is(err, errForbidden):
		// 403 不解释「缺哪个权限」——那等于把权限模型念给不该知道的人听。
		writeError(w, http.StatusForbidden, api.Forbidden, "没有审核权限")
	case errors.Is(err, demo.ErrNotFound):
		writeError(w, http.StatusNotFound, api.NotFound, "作品不存在")
	case errors.Is(err, demo.ErrNotPending):
		// 409 而不是 400：请求本身没问题，是**作品当前不在这条路上**。
		// 最常见的成因是两个人同时在审同一条，或者审完又点了一次。
		writeError(w, http.StatusConflict, api.Conflict, messageOf(err))
	case errors.Is(err, demo.ErrInvalidInput):
		writeError(w, http.StatusBadRequest, api.InvalidParam, messageOf(err))
	default:
		// 未分类的错误要留痕。否则线上只会看到一句「服务暂时不可用」，
		// 日志里一片干净——排查时连是哪一层炸的都无从知道。
		slog.Error("审核链路未分类错误", "err", err)
		writeError(w, http.StatusServiceUnavailable, api.Unavailable, "服务暂时不可用")
	}
}

// ListDemoReviewQueue 返回待审队列。
func (h *Handler) ListDemoReviewQueue(w http.ResponseWriter, r *http.Request, params api.ListDemoReviewQueueParams) {
	if h.demos == nil {
		writeError(w, http.StatusServiceUnavailable, api.Unavailable, "服务暂时不可用")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), queryTimeout)
	defer cancel()

	if _, err := h.reviewer(ctx, r); err != nil {
		h.writeReviewError(w, err)
		return
	}

	limit := int32(50)
	if params.Limit != nil {
		limit = int32(*params.Limit)
	}

	rows, err := h.demos.ListPendingReviews(ctx, limit)
	if err != nil {
		h.writeReviewError(w, err)
		return
	}

	items := make([]api.ReviewQueueItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, api.ReviewQueueItem{
			Slug: row.Slug,
			// 三处 *string 是「作者注销 / 字段为空」的对外表达：给 null 而不是空串，
			// 让审核员分得清「没写」和「写了空」。
			Title:           nullableString(row.Title),
			Description:     nullableString(row.Description),
			AuthorName:      nullableString(row.AuthorName),
			ModerationState: api.ReviewQueueItemModerationState("pending"),
			SubmittedAt:     row.SubmittedAt,
			PreviewPath:     previewPath(row.Slug, row.CurrentRevNo),
		})
	}
	writeJSON(w, http.StatusOK, api.ReviewQueue{Items: items})
}

// ReviewDemo 裁决一条待审作品。
func (h *Handler) ReviewDemo(w http.ResponseWriter, r *http.Request, slug string) {
	if h.demos == nil {
		writeError(w, http.StatusServiceUnavailable, api.Unavailable, "服务暂时不可用")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), queryTimeout)
	defer cancel()

	actor, err := h.reviewer(ctx, r)
	if err != nil {
		h.writeReviewError(w, err)
		return
	}

	var body api.ReviewDecision
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, api.InvalidParam, err.Error())
		return
	}

	// 裁决值必须显式校验。契约里 ReviewDecision.decision 是 required + enum，
	// 而 decodeJSON 的 DisallowUnknownFields 只管字段名、不管枚举值——
	// 少了这段，decision:"yes" 会因为「不等于 approve」被当成驳回，还返回 200。
	var approve bool
	switch body.Decision {
	case api.Approve:
		approve = true
	case api.Reject:
		approve = false
	default:
		writeError(w, http.StatusBadRequest, api.InvalidParam, "decision 只能是 approve 或 reject")
		return
	}

	reason := ""
	if body.Reason != nil {
		reason = *body.Reason
	}

	out, err := h.demos.Review(ctx, actor, demo.ReviewInput{
		Slug:    slug,
		Approve: approve,
		Reason:  reason,
	})
	if err != nil {
		h.writeReviewError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, api.ReviewResult{
		Slug:            out.Slug,
		ModerationState: api.ReviewResultModerationState(out.ModerationState),
	})
}

// previewPath 给出这一版在预览服务下的地址（Demo 域 §7.5）。
// 没有版本号时（理论上不会发生在待审队列里）退回不带版本的形式。
func previewPath(slug string, revNo int) string {
	if revNo > 0 {
		return "/preview/" + slug + "/v" + strconv.Itoa(revNo) + "/"
	}
	return "/preview/" + slug + "/"
}

// nullableString 把空串变成 nil，让 JSON 里出现 null 而不是 ""。
// 审核队列里这三个字段都可能为空（作者注销、描述没写），
// 空串会让审核员以为「作者名字就是一串空白」。
func nullableString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
