package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/demo"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/identity"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// 发布端点的传输层。
//
// # 为什么 `demo.publish` 不复用 `demo.review`
//
// 审核与发布是两个判断：「这份内容能不能对外」和「现在把它挂到哪个渠道、以什么可见性」。
// 合成一个权限的后果是：想让某个人只审不发做不到，想让某个人只发不审也做不到。
// 二者的风险也不一样——审错是放进来一个坏作品，发错是把它挂到首页。
//
// # 为什么状态冲突一律 409 而不是 400
//
// 请求本身是合法的，是**作品当前不在这条路上**：没审过、已经发布过、已被移除。
// 400 会让调用方去翻自己的请求体，而真正该做的事是刷新一下看当前状态。

// publisher 是发布链路上唯一的鉴权点。
func (h *Handler) publisher(ctx context.Context, r *http.Request) (demo.Actor, error) {
	if h.identity == nil || h.queries == nil {
		return demo.Actor{}, errNotWired
	}
	account, err := h.identity.Me(ctx, readSessionCookie(r))
	if err != nil {
		return demo.Actor{}, err
	}
	ok, err := h.queries.HasPermission(ctx, store.HasPermissionParams{
		Sub:           account.Sub,
		PermissionKey: "demo.publish",
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

// writePublishError 把发布链路上的错误映射成状态码。
func (h *Handler) writePublishError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, identity.ErrUnauthorized):
		writeError(w, http.StatusUnauthorized, api.Unauthorized, "请先登录")
	case errors.Is(err, errForbidden):
		writeError(w, http.StatusForbidden, api.Forbidden, "没有发布权限")
	case errors.Is(err, demo.ErrNotFound):
		writeError(w, http.StatusNotFound, api.NotFound, "作品不存在")
	case errors.Is(err, demo.ErrNotApproved),
		errors.Is(err, demo.ErrAlreadyPublished),
		errors.Is(err, demo.ErrRemovedCannotPublish),
		errors.Is(err, demo.ErrPublishConflict):
		// 四者都是「这条作品现在不在这条路上」，不是请求写错了。
		writeError(w, http.StatusConflict, api.Conflict, messageOf(err))
	case errors.Is(err, demo.ErrInvalidVisibility),
		errors.Is(err, demo.ErrInvalidChannel),
		errors.Is(err, demo.ErrInvalidInput):
		writeError(w, http.StatusBadRequest, api.InvalidParam, messageOf(err))
	default:
		writeError(w, http.StatusServiceUnavailable, api.Unavailable, "服务暂时不可用")
	}
}

// PublishDemo 把一条已过审的作品发布到一个渠道。
func (h *Handler) PublishDemo(w http.ResponseWriter, r *http.Request, slug string) {
	if h.demos == nil {
		writeError(w, http.StatusServiceUnavailable, api.Unavailable, "服务暂时不可用")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), queryTimeout)
	defer cancel()

	actor, err := h.publisher(ctx, r)
	if err != nil {
		h.writePublishError(w, err)
		return
	}

	var body api.PublishRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, api.InvalidParam, err.Error())
		return
	}

	lang := "zh"
	if body.Lang != nil {
		lang = string(*body.Lang)
	}

	out, err := h.demos.Publish(ctx, actor, demo.PublishInput{
		Slug:       slug,
		Channel:    body.Channel,
		Visibility: string(body.Visibility),
		Lang:       lang,
	})
	if err != nil {
		h.writePublishError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, api.PublishResult{
		Slug:           out.Slug,
		LifecycleState: api.PublishResultLifecycleState(out.LifecycleState),
		Channel:        out.Channel,
		Visibility:     api.PublishResultVisibility(out.Visibility),
	})
}
