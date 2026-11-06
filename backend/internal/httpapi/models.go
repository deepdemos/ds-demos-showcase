package httpapi

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"time"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/model"
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

// modelOfDetailRow 与 modelOf 同形，只是行类型来自另一条 SQL。
//
// `ListModels` 与 `GetModelByID` 的列集逐字一致（同一个视图口径），但 sqlc 给的是
// 两个不同的结构体。转一下比让调用方各写一份字段映射便宜——也避免「详情少一个字段」
// 这种只在某个入口才出现的偏差。
func modelOfDetailRow(m store.GetModelByIDRow, aliases []string) api.Model {
	return modelOf(store.ListModelsRow(m), aliases)
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

// syncTimeout 是手动同步的上限：拉包 60 秒 + 写入几秒，留一倍余量。
const syncTimeout = 2 * time.Minute

// SyncModelCatalog 立即同步一次上游目录镜像。
//
// 与定时同步共用 `model.Service.Sync`：两条触发路径必须有同一套保护（抢锁、
// payload 未变不重写、失败保留旧镜像）。在这里再写一份「先删后插」，
// 两处一定会慢慢长歪。
func (h *Handler) SyncModelCatalog(w http.ResponseWriter, r *http.Request) {
	if h.queries == nil || h.models == nil {
		writeError(w, http.StatusServiceUnavailable, api.Unavailable, "服务暂时不可用")
		return
	}
	if _, err := h.modelImporter(r.Context(), r); err != nil {
		h.writeModelError(w, err)
		return
	}

	// 同步要拉 6 MB 再写 9000 行，5 秒的查询上限不够；但也不能没有上限，
	// 否则上游挂住时这个 handler 会一直占着一个连接。
	ctx, cancel := context.WithTimeout(r.Context(), syncTimeout)
	defer cancel()

	res, err := h.models.Sync(ctx)
	if err != nil {
		switch {
		case errors.Is(err, model.ErrSyncBusy):
			// 409 而不是 503：这不是「服务坏了」，是「你要的事已经有人在做了」。
			// 排队等也没有意义——等到的下一轮已经过时。
			writeError(w, http.StatusConflict, api.Conflict, "另一个同步正在进行，稍后再试")
		default:
			// 502 而不是 500：失败几乎都来自上游（不可达、返回非 JSON、字段形状变了），
			// 而本地镜像保持原样。用 500 会让运维先怀疑自己的库。
			writeError(w, http.StatusBadGateway, api.Unavailable, "同步上游目录失败："+err.Error())
		}
		return
	}

	out := api.ModelSyncResult{
		Source:   catalogSource,
		Status:   model.StatusOK,
		Items:    res.Items,
		Skipped:  res.Skipped,
		SyncedAt: res.At,
	}
	writeJSON(w, http.StatusOK, out)
}

// catalogSource 与 `catalog.Source` 一致。
//
// 单独抄一份常量是为了让 httpapi 不必 import platform/catalog 只为拿一个字符串；
// `scripts/check-single-source.mjs` 不管这种常量，所以两边取值由测试核对
// （`TestCatalogSourceMatchesPlatform`）。
const catalogSource = "models.dev"
