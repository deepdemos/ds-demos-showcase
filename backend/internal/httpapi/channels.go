package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/demo"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/identity"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
	"github.com/jackc/pgx/v5"
)

// 渠道上架 / 撤下：给已经对外发布的作品追加或移除一个渠道。
//
// 权限用 `demo.curate`（#508 统一）。渠道行回答的是「这件作品在这个渠道里露不露」，
// 那是**策展**动作；`demo.publish` 只管状态跃迁（`draft`/`archived` → `published`）。
// 这三个端点原来用 `demo.publish`，与标准对象 §EN-1、治理域 §4.3 两处文档相反，
// 而 `demo.curate` 当时全仓没有使用者 —— 改用它是把文档与代码收口，不是新授权：
// 加渠道本身仍要求内容已审核、作品已有对外版本。

// curator 是渠道链路上唯一的鉴权点。
func (h *Handler) curator(ctx context.Context, r *http.Request) (demo.Actor, error) {
	if h.identity == nil || h.queries == nil {
		return demo.Actor{}, errNotWired
	}
	account, err := h.identity.Me(ctx, readSessionCookie(r))
	if err != nil {
		return demo.Actor{}, err
	}
	ok, err := h.queries.HasPermission(ctx, store.HasPermissionParams{
		Sub:           account.Sub,
		PermissionKey: "demo.curate",
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

// AddDemoChannel 给作品追加一个渠道，或改某个渠道的可见性。
func (h *Handler) AddDemoChannel(w http.ResponseWriter, r *http.Request, slug string) {
	actor, err := h.curator(r.Context(), r)
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
	actor, err := h.curator(r.Context(), r)
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
		writeError(w, http.StatusForbidden, api.Forbidden, "没有渠道策展权限")
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

// ListDemoChannels 返回作品挂在哪些渠道上（管理端读）。
//
// 不塞进详情：详情是公开读的出口，字段面讲的是「读者看到的这一件作品」，
// 渠道是运营视角。混在一起要么让公开字段变胖，要么要按角色裁剪同一份 schema。
func (h *Handler) ListDemoChannels(w http.ResponseWriter, r *http.Request, slug string) {
	if _, err := h.curator(r.Context(), r); err != nil {
		writeChannelError(w, r, err)
		return
	}

	rows, err := h.queries.ListDemoChannelsForSlug(r.Context(), slug)
	if err != nil {
		slog.Error("读渠道列表失败", "err", err)
		writeError(w, http.StatusServiceUnavailable, api.Unavailable, "服务暂时不可用")
		return
	}

	// 作品不存在与「没有渠道」必须分开：前者 404，后者是空数组。
	// 列表查询在没有渠道时也返回 0 行，所以先单独确认作品存在。
	if len(rows) == 0 {
		if _, err := h.queries.GetDemoForChannel(r.Context(), slug); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeError(w, http.StatusNotFound, api.NotFound, "作品不存在")
				return
			}
			slog.Error("读作品状态失败", "err", err)
			writeError(w, http.StatusServiceUnavailable, api.Unavailable, "服务暂时不可用")
			return
		}
	}

	items := make([]api.DemoChannel, 0, len(rows))
	for _, row := range rows {
		items = append(items, api.DemoChannel{
			Channel:    row.Channel,
			Visibility: api.DemoChannelVisibility(row.Visibility),
			Lang:       api.DemoChannelLang(row.Lang),
		})
	}
	writeJSON(w, http.StatusOK, api.DemoChannelList{Items: items})
}
