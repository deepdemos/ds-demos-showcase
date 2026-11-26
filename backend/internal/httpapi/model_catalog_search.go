package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// upstreamSearchLimit 是公开上游搜索的返回条数上限（运行手册第 54 条）。
//
// 不分页是设计口径：这是模型选择器的数据源，不是供人逐页浏览的目录——
// 「一次给足候选」比「翻页」更贴近选择器的用法。改它要同步契约描述
// （api/openapi.yaml 的 searchUpstreamModels）与手册那一行。
const upstreamSearchLimit = 20

// SearchUpstreamModels 对应 GET /api/v1/models/upstream（公开只读）。
//
// # 为什么公开
//
// 返回的只有上游公开目录里的名称、发布方、规范标识与上游状态，外加「站内有没有收录」
// 这一位（#590 的选择器要据此把条目提到第一层）。导入溯源不在响应里，
// 所以公开不构成泄漏：谁导的、什么时候导的只出现在管理端审计。
//
// # 为什么与作者搜索共用一套转义
//
// 用户搜 `100%` 想知道的是带百分号的模型，不是「100 后面随便什么」；不转义的话
// `q=%` 会把整份镜像吐回来。口径只有一套（handlers.go 的 likeEscaper），这里不另写。
//
// # 为什么不在这里过滤上游已弃用
//
// 上游 `upstream_status='deprecated'`（厂商不再维护）与我们自己的
// `models.status='deprecated'`（已合并到别的模型）是两套语义。选择器要能搜到
// 灰测 / 弃用名，所以不过滤；要不要标「上游弃用」由调用方看 `upstreamStatus` 决定。
func (h *Handler) SearchUpstreamModels(w http.ResponseWriter, r *http.Request, params api.SearchUpstreamModelsParams) {
	if h.queries == nil {
		unavailable(w, errors.New("database not configured"))
		return
	}

	// 空与全空格等同「没传」：调用方清空搜索框时不该得到 400。
	q := ""
	if params.Q != nil {
		q = strings.TrimSpace(*params.Q)
	}
	if len([]rune(q)) > maxSearchLen {
		writeError(w, http.StatusBadRequest, api.InvalidParam, "搜索词太长，最多 100 个字")
		return
	}
	// 长度按**用户输入**判定（上面那行），转义放在判定之后：转义会改变字符数，
	// 不该让「搜一串百分号」比「搜同样多的汉字」更容易被拒。
	var needle pgtype.Text
	if q != "" {
		needle = pgtype.Text{String: likeEscaper.Replace(q), Valid: true}
	}

	ctx, cancel := context.WithTimeout(r.Context(), queryTimeout)
	defer cancel()

	rows, err := h.queries.SearchUpstreamModels(ctx, store.SearchUpstreamModelsParams{
		Q:        needle,
		RowLimit: upstreamSearchLimit,
	})
	if err != nil {
		unavailable(w, err)
		return
	}

	items := make([]api.UpstreamModel, 0, len(rows))
	for _, row := range rows {
		items = append(items, api.UpstreamModel{
			Source:         row.Source,
			ProviderId:     row.ProviderID,
			ProviderName:   row.ProviderName,
			ExternalId:     row.ExternalID,
			Name:           row.Name,
			CanonicalId:    row.CanonicalID.String,
			UpstreamStatus: row.UpstreamStatus.String,
			// 空串是「未收录」（models.slug 有 CHECK 1..128，不可能为空）：
			// 契约里未收录是 null，选择器用它分三层，所以这里必须折成 nil。
			Slug: strPtrOf(row.ModelSlug),
		})
	}
	writeJSON(w, http.StatusOK, api.UpstreamModelList{Items: items})
}
