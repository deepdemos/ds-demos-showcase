package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/demo"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/identity"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// 管理端模型列表（#596 / 模型域 §6.7）。
//
// # 为什么不复用公开的 GET /api/v1/models
//
// 公开列表只返回身份字段，不带档案行（model_specs）；管理端列表要同时展示厂商、
// 状态、档案完整度（缺失 / 部分 / 完整）与已下架标记。档案完整度按规格**不落库、
// 不进契约**，由前端从 specs 单一函数推导，因此后端只负责把 models 与 model_specs
// 如实拼在一起返回（没有档案行时 specs 为 null，不是空对象）。

// parseAdminModelStatus 校验并转换管理端列表的可选 status 查询参数。
//
// oapi-codegen 只做类型转换不做枚举校验：若不在此处把关，非法值会原样传进 SQL
// 变成「空列表 200」，调用方以为没有数据，其实是参数拼错了。
func parseAdminModelStatus(raw *string) (pgtype.Text, bool) {
	if raw == nil || *raw == "" {
		return pgtype.Text{}, true
	}
	switch api.ModelStatus(*raw) {
	case api.ModelStatusCandidate,
		api.ModelStatusActive,
		api.ModelStatusUnverified,
		api.ModelStatusDeprecated:
		return pgtype.Text{String: *raw, Valid: true}, true
	default:
		return pgtype.Text{}, false
	}
}

// requireAnyPermission 校验调用方至少持有 keys 中的一个权限键。
//
// 管理端模型列表同时服务于导入、手动新建与后续治理（#597），把任意一个模型管理权限
// 绑死都会挡住只持有另外一把键的角色。
func (h *Handler) requireAnyPermission(ctx context.Context, r *http.Request, keys ...string) (demo.Actor, error) {
	if h.identity == nil || h.queries == nil {
		return demo.Actor{}, errNotWired
	}
	account, err := h.identity.Me(ctx, readSessionCookie(r))
	if err != nil {
		return demo.Actor{}, err
	}
	for _, key := range keys {
		ok, err := h.queries.HasPermission(ctx, store.HasPermissionParams{
			Sub:           account.Sub,
			PermissionKey: key,
		})
		if err != nil {
			return demo.Actor{}, err
		}
		if ok {
			return demo.Actor{
				AccountID: account.ID,
				UserID:    account.UserID,
				Role:      account.Role,
			}, nil
		}
	}
	return demo.Actor{}, errForbidden
}

// modelSpecsMap 一次读出全部模型档案并按 model_id 建表。
//
// 复用 model_page.go 的 specsOf（ListModelSpecsRow 与 GetModelSpecsRow 列集逐字一致），
// 避免列表与详情各写一份字段映射导致某一边悄悄漏列。
func (h *Handler) modelSpecsMap(ctx context.Context) (map[int64]*api.ModelSpecs, error) {
	rows, err := h.queries.ListModelSpecs(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[int64]*api.ModelSpecs, len(rows))
	for _, s := range rows {
		out[s.ModelID] = specsOf(store.GetModelSpecsRow(s))
	}
	return out, nil
}

// ListAdminModels 对应 GET /api/v1/admin/models。
func (h *Handler) ListAdminModels(w http.ResponseWriter, r *http.Request, params api.ListAdminModelsParams) {
	if h.queries == nil || h.identity == nil {
		writeError(w, http.StatusServiceUnavailable, api.Unavailable, "服务暂时不可用")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), queryTimeout)
	defer cancel()

	if _, err := h.requireAnyPermission(ctx, r, permModelImport, permModelCreate, permModelGovern); err != nil {
		switch {
		case errors.Is(err, identity.ErrUnauthorized):
			writeError(w, http.StatusUnauthorized, api.Unauthorized, "请先登录")
		case errors.Is(err, errForbidden):
			writeError(w, http.StatusForbidden, api.Forbidden, "没有模型管理权限")
		case errors.Is(err, errNotWired):
			writeError(w, http.StatusServiceUnavailable, api.Unavailable, "服务暂时不可用")
		default:
			unavailable(w, err)
		}
		return
	}

	status, ok := parseAdminModelStatus(params.Status)
	if !ok {
		writeError(w, http.StatusBadRequest, api.InvalidParam,
			"status 只允许 candidate / active / unverified / deprecated")
		return
	}

	rows, err := h.queries.ListModels(ctx, status)
	if err != nil {
		unavailable(w, err)
		return
	}
	aliases, err := h.modelAliases(ctx)
	if err != nil {
		unavailable(w, err)
		return
	}
	specs, err := h.modelSpecsMap(ctx)
	if err != nil {
		unavailable(w, err)
		return
	}

	items := make([]api.AdminModelRow, 0, len(rows))
	for _, m := range rows {
		items = append(items, api.AdminModelRow{
			Model: modelOf(m, aliases[m.ID]),
			Specs: specs[m.ID],
		})
	}
	writeJSON(w, http.StatusOK, api.AdminModelList{Items: items})
}
