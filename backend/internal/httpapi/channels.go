package httpapi

import (
	"errors"
	"net/http"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/demo"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/identity"
)

// 渠道上架 / 撤下：给已经对外发布的作品追加或移除一个渠道。
//
// 权限用 `demo.publish`——这是「让作品出现在某个渠道上」的动作，与首次发布同一件事的
// 后续步骤，不另立权限键（多一个键就多一处要维护、多一种配置错法）。松点 26 结案。

// AddDemoChannel 给作品追加一个渠道，或改某个渠道的可见性。
func (h *Handler) AddDemoChannel(w http.ResponseWriter, r *http.Request, slug string) {
	actor, err := h.publisher(r.Context(), r)
	if err != nil {
		writeChannelError(w, r, err)
		return
	}

	// 与本仓其它写端点一致：请求体在 handler 里解，生成出来的接口不带 body 参数
	// （见 api/oapi-codegen-server.yaml，契约仍然完整描述请求体）。
	var body api.DemoChannelRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, api.InvalidParam, err.Error())
		return
	}

	res, err := h.demos.AddChannel(r.Context(), actor, demo.ChannelInput{
		Slug:       slug,
		Channel:    body.Channel,
		Visibility: string(body.Visibility),
		Lang:       langPtrOf(body.Lang),
	})
	if err != nil {
		writeChannelError(w, r, err)
		return
	}

	status := http.StatusCreated
	if !res.Created {
		// 200 = 这个渠道本来就有，这次改的是可见性。调用方据此能区分「加了」与「改了」。
		status = http.StatusOK
	}
	writeJSON(w, status, api.DemoChannelResult{
		Slug:       res.Slug,
		Channel:    res.Channel,
		Visibility: api.DemoChannelResultVisibility(res.Visibility),
		Lang:       api.DemoChannelResultLang(res.Lang),
		Created:    res.Created,
	})
}

// RemoveDemoChannel 从某个渠道撤下，作品可以仍有别的渠道。
func (h *Handler) RemoveDemoChannel(w http.ResponseWriter, r *http.Request, slug string, channel string) {
	actor, err := h.publisher(r.Context(), r)
	if err != nil {
		writeChannelError(w, r, err)
		return
	}
	if err := h.demos.RemoveChannel(r.Context(), actor, slug, channel); err != nil {
		writeChannelError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// writeChannelError 把领域错误翻成状态码。409 的三种情况都给不同的原话：
// 「还没对外过」「内容没通过审核」「这是最后一个渠道」——同一个码，三种下一步。
func writeChannelError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, identity.ErrUnauthorized):
		writeError(w, http.StatusUnauthorized, api.Unauthorized, "请先登录")
	case errors.Is(err, errForbidden):
		writeError(w, http.StatusForbidden, api.Forbidden, "没有发布权限")
	case errors.Is(err, errNotWired):
		writeError(w, http.StatusServiceUnavailable, api.Unavailable, "服务暂时不可用")
	case errors.Is(err, demo.ErrInvalidInput),
		errors.Is(err, demo.ErrInvalidVisibility),
		errors.Is(err, demo.ErrInvalidChannel):
		writeError(w, http.StatusBadRequest, api.InvalidParam, err.Error())
	case errors.Is(err, demo.ErrNotFound), errors.Is(err, demo.ErrChannelNotFound):
		writeError(w, http.StatusNotFound, api.NotFound, err.Error())
	case errors.Is(err, demo.ErrChannelNotPublished),
		errors.Is(err, demo.ErrNotApproved),
		errors.Is(err, demo.ErrLastChannel):
		writeError(w, http.StatusConflict, api.Conflict, err.Error())
	default:
		writeError(w, http.StatusServiceUnavailable, api.Unavailable, "服务暂时不可用")
	}
}

// 可选语言指针拆开。空 = 交给领域层填默认值：默认值是领域规则，不是传输层的事。
func langPtrOf(v *api.DemoChannelRequestLang) string {
	if v == nil {
		return ""
	}
	return string(*v)
}
