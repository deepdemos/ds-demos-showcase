package httpapi

import (
	"context"
	"errors"
	"net/http"
	"sort"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
	"github.com/jackc/pgx/v5/pgtype"
)

// ListModels 返回模型目录。
//
// **默认返回全部状态**：早先这里写死 `status = 'active'`，于是契约里的 `status`
// 字段永远等于同一个值——那等于给了个常量。目录页要能展示「候选 / 未核实 /
// 已退役并指向合并归宿」，这个筛选交给调用方（表单那边自己传 `?status=active`）。
//
// 不分页：模型目录是受控的少量行（外部目录导入的一部分），不是用户内容。
// 到几百行的规模时它会先需要一个管理界面，那时再谈分页。
func (h *Handler) ListModels(w http.ResponseWriter, r *http.Request, params api.ListModelsParams) {
	if h.queries == nil {
		unavailable(w, errors.New("database not configured"))
		return
	}

	var status pgtype.Text
	if params.Status != nil {
		status = pgtype.Text{String: string(*params.Status), Valid: true}
	}

	ctx, cancel := context.WithTimeout(r.Context(), queryTimeout)
	defer cancel()

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

	items := make([]api.Model, 0, len(rows))
	for _, m := range rows {
		items = append(items, modelOf(m, aliases[m.ID]))
	}
	writeJSON(w, http.StatusOK, api.ModelList{Items: items})
}

// modelAliases 把别名表读成 model_id → 别名数组。
//
// 读全量而不是「每个模型一次查询」：别名是解析入口，行数比模型少一个量级
// （同一个模型五六种写法），一条 SQL 比 N+1 省。
func (h *Handler) modelAliases(ctx context.Context) (map[int64][]string, error) {
	rows, err := h.queries.ListModelAliases(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[int64][]string, len(rows))
	for _, a := range rows {
		out[a.ModelID] = append(out[a.ModelID], a.Alias)
	}
	// 查询里已经 ORDER BY model_id, alias 了，这里再排一次是防「以后有人改排序」：
	// 别名的顺序进 JSON，调用方会拿它做 diff。
	for _, v := range out {
		sort.Strings(v)
	}
	return out, nil
}

// modelOf 把一行目录数据摊成契约里的 Model。
//
// 详情里的模型对象走同一个函数：同一份数据在两处长得不一样，前端就得写两套渲染。
func modelOf(m store.ListModelsRow, aliases []string) api.Model {
	if aliases == nil {
		aliases = []string{}
	}
	return api.Model{
		Slug:           m.Slug,
		Name:           m.Name,
		Vendor:         m.Vendor,
		Status:         api.ModelStatus(m.Status),
		Resolution:     api.ModelResolution(m.Resolution),
		MergedIntoSlug: m.MergedIntoSlug,
		Source:         api.ModelSource(m.Source),
		Aliases:        aliases,
		Description:    m.Description,
	}
}
