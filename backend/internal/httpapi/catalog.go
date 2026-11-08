// Package httpapi 实现 api/openapi.yaml 定义的端点。
//
// 分层：这个包只负责「HTTP ↔ 领域判定」，SQL 在 internal/store（sqlc 生成）。
// 路由注册不在这里，在 internal/httpapi/router.go——那里把实现塞进生成的路由表。
package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// classificationsOf 读一件作品的分类（详情页用）。
//
// 与 modelsOf 的两点不同：①分类不随版本变，所以不判 viewerIsAuthor；
// ②出错只记日志并返回空数组——分类读不出来不该让整个详情页 503，
// 那会把「少一行标注」升级成「页面打不开」。
func (h *Handler) classificationsOf(ctx context.Context, demoID int64) []api.ClassificationRef {
	out := []api.ClassificationRef{}
	if demoID <= 0 {
		return out
	}
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	rows, err := h.queries.ListDemoClassifications(ctx, demoID)
	if err != nil {
		slog.Error("读作品分类失败", "demo", demoID, "err", err)
		return out
	}
	for _, r := range rows {
		out = append(out, api.ClassificationRef{
			Dimension: api.ClassificationRefDimension(r.Dimension),
			Slug:      r.Slug,
			Label:     r.Label,
		})
	}
	return out
}

// filterSlugs 把请求里的筛选值整理成「去空、去重、限长」的一批 slug。
//
// 返回 false 表示某一维超过了契约声明的 50 个：那说明调用方在拼 URL 时失控了，
// 与其让它变成一条 50 个 OR 的 SQL（PG 能跑，但计划会烂），不如直接 400。
//
// 不做「slug 是否存在」的校验：筛选是查询，未收录的 slug 命中 0 行就够了。
// 报 400 会把「用户改 URL 试试看」变成一堆红字，而那正是浏览筛选器的正常用法。
func filterSlugs(raw *[]string) ([]string, bool) {
	if raw == nil {
		return []string{}, true
	}
	seen := make(map[string]struct{}, len(*raw))
	out := make([]string, 0, len(*raw))
	for _, v := range *raw {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, dup := seen[v]; dup {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	if len(out) > maxFilterValues {
		return nil, false
	}
	return out, true
}

// classificationsFor 一次取回整页作品的分类，按 demo_id 分组。
//
// 一件作品一次查询会在 pageSize=50 时变成 51 次往返；这条查询只跑一次。
// 出错只记日志并返回空分类：列表本身还有用，不该因为分类读不出来整页 503。
func (h *Handler) classificationsFor(ctx context.Context, rows []store.ListPublishedDemosRow) map[int64][]api.ClassificationRef {
	out := make(map[int64][]api.ClassificationRef, len(rows))
	if len(rows) == 0 {
		return out
	}

	ids := make([]int64, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}

	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	cls, err := h.queries.ListClassificationsForDemos(ctx, ids)
	if err != nil {
		slog.Error("读作品分类失败", "count", len(ids), "err", err)
		return out
	}
	for _, c := range cls {
		out[c.DemoID] = append(out[c.DemoID], api.ClassificationRef{
			Dimension: api.ClassificationRefDimension(c.Dimension),
			Slug:      c.Slug,
			Label:     c.Label,
		})
	}
	// 每一行都要有非 nil 的数组：契约里是 required，null 会让前端多一条分支。
	for _, row := range rows {
		if _, ok := out[row.ID]; !ok {
			out[row.ID] = []api.ClassificationRef{}
		}
	}
	return out
}

// ListClassifications 对应 GET /api/v1/classifications。
//
// 一次返回全部三个维度：92 条分类的响应只有几 KB，分页反而是负担——
// 筛选器需要一次拿到全量才画得出来，分页会让它要么发三次请求、要么先画半张。
func (h *Handler) ListClassifications(w http.ResponseWriter, r *http.Request) {
	if h.queries == nil {
		unavailable(w, errors.New("database not configured"))
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), queryTimeout)
	defer cancel()

	dims, err := h.queries.ListDimensions(ctx)
	if err != nil {
		unavailable(w, err)
		return
	}
	items, err := h.queries.ListClassifications(ctx)
	if err != nil {
		unavailable(w, err)
		return
	}

	// slug 索引：parent_id 是 id，响应里要的是 slug。
	slugByID := make(map[int64]string, len(items))
	for _, it := range items {
		slugByID[it.ID] = it.Slug
	}

	out := api.ClassificationCatalog{
		Dimensions: make([]struct {
			Description *string                                      `json:"description,omitempty"`
			Dimension   api.ClassificationCatalogDimensionsDimension `json:"dimension"`
			Label       string                                       `json:"label"`
			Mode        api.ClassificationCatalogDimensionsMode      `json:"mode"`
		}, 0, len(dims)),
		Items: make([]api.Classification, 0, len(items)),
	}
	for _, d := range dims {
		desc := d.Description
		out.Dimensions = append(out.Dimensions, struct {
			Description *string                                      `json:"description,omitempty"`
			Dimension   api.ClassificationCatalogDimensionsDimension `json:"dimension"`
			Label       string                                       `json:"label"`
			Mode        api.ClassificationCatalogDimensionsMode      `json:"mode"`
		}{
			Description: &desc,
			Dimension:   api.ClassificationCatalogDimensionsDimension(d.Dimension),
			Label:       d.Label,
			Mode:        api.ClassificationCatalogDimensionsMode(d.Mode),
		})
	}

	for _, it := range items {
		item := api.Classification{
			Dimension:   api.ClassificationDimension(it.Dimension),
			Slug:        it.Slug,
			Label:       it.Label,
			DemoCount:   int(it.DemoCount),
			Description: &it.Description,
			Aliases:     &it.Aliases,
		}
		if it.ParentID.Valid {
			if parent, ok := slugByID[it.ParentID.Int64]; ok {
				item.ParentSlug = &parent
			}
		}
		out.Items = append(out.Items, item)
	}

	writeJSON(w, http.StatusOK, out)
}
