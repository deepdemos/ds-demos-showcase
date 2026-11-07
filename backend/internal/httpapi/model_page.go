package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// 模型页的传输层（模型域 §5.1）。
//
// # 一个端点返回三块，而不是三个端点
//
// 三块的数据来自同一个模型、同一次打开页面。拆成三个端点的话，页面要打三次请求、
// 处理三种失败、再想办法对齐「这三份是同一个时刻的」。而这个页面的三块都只读、
// 都很快，没有拆的理由。
//
// # 档案没有时给 null，不给空对象
//
// `model_specs` 一行都没有，与「有一行但每栏都空」对读者是两件事：前者是「还没录入」，
// 后者是「录了，但上游也没给」。前者界面该说「还没有档案」，后者该排一排「—」。

// GetModel 是模型页的唯一读端点。
func (h *Handler) GetModel(w http.ResponseWriter, r *http.Request, slug string) {
	if h.queries == nil {
		unavailable(w, errors.New("database not configured"))
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), queryTimeout)
	defer cancel()

	row, err := h.queries.GetModelBySlug(ctx, slug)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, api.NotFound, "没有这个模型")
			return
		}
		unavailable(w, err)
		return
	}

	out := api.ModelProfile{
		Model: api.Model{
			Slug:           row.Slug,
			Name:           row.Name,
			Vendor:         row.Vendor,
			Status:         api.ModelStatus(row.Status),
			Resolution:     api.ModelResolution(row.Resolution),
			MergedIntoSlug: row.MergedIntoSlug,
			Description:    row.Description,
			Source:         api.ModelSource(row.Source),
			Aliases:        h.aliasesOf(ctx, row.ID),
		},
		Demos:      []api.ModelDemoRef{},
		Providers:  []api.ModelProviderSlice{},
		Benchmarks: []api.ModelBenchmarkScore{},
		Site:       api.ModelSiteStats{},
	}
	// 站内表现：作品数 / 票数 / 社区分。
	stats, err := h.queries.ModelSiteStats(ctx, row.ID)
	if err != nil {
		unavailable(w, err)
		return
	}
	out.Site = api.ModelSiteStats{
		DemoCount:   int(stats.DemoCount),
		Votes:       int(stats.Votes),
		Scored:      stats.Scored,
		Score:       stats.Score,
		PriorWeight: stats.PriorWeight,
	}

	// 档案：没有行就保持 nil。
	if specs, err := h.queries.GetModelSpecs(ctx, row.ID); err == nil {
		out.Specs = specsOf(specs)
	} else if !errors.Is(err, pgx.ErrNoRows) {
		unavailable(w, err)
		return
	}

	// 作品列表：判据与作品库同一份（demo_public_demos 视图）。
	demos, err := h.queries.ListModelDemos(ctx, row.ID)
	if err != nil {
		unavailable(w, err)
		return
	}
	for _, d := range demos {
		out.Demos = append(out.Demos, api.ModelDemoRef{
			Slug:          d.Slug,
			Title:         d.Title.String,
			CreatedAt:     d.CreatedAt.Time,
			RatingAverage: d.RatingAvg,
			RatingCount:   int(d.RatingCount),
		})
	}

	// provider 分布：只读展示，不参与上面的社区分。
	slices, err := h.queries.ListModelProviders(ctx, row.ID)
	if err != nil {
		unavailable(w, err)
		return
	}
	for _, p := range slices {
		item := api.ModelProviderSlice{
			ProviderSlug: p.ProviderSlug,
			ProviderName: p.ProviderName,
			DemoCount:    int(p.DemoCount),
			Votes:        int(p.Votes),
		}
		if p.ProviderID.Valid {
			id := int(p.ProviderID.Int64)
			item.ProviderId = &id
		}
		// 票数为 0 时不展示均分（样本太小，容易被读成「这个 provider 只有 0 分」）。
		if p.Votes > 0 {
			avg := p.AvgRating
			item.AvgRating = &avg
		}
		out.Providers = append(out.Providers, item)
	}

	// 第三方基准：只列参与综合分资格的条目。
	scores, err := h.queries.ListModelBenchmarks(ctx, row.ID)
	if err != nil {
		unavailable(w, err)
		return
	}
	for _, b := range scores {
		item := api.ModelBenchmarkScore{
			BenchmarkSlug: b.BenchmarkSlug,
			BenchmarkName: b.BenchmarkName,
			Version:       b.Version,
			AsOf:          openapi_types.Date{Time: b.AsOf.Time},
			SourceUrl:     strPtrOrNil(b.SourceUrl),
			MetricKey:     b.MetricKey,
			MetricLabel:   b.MetricLabel,
			Unit:          strPtrOrNil(b.Unit),
			RawScore:      b.RawScore,
		}
		if b.ParticipantCount.Valid {
			n := int(b.ParticipantCount.Int64)
			item.ParticipantCount = &n
		}
		if b.Rank.Valid {
			n := int(b.Rank.Int32)
			item.Rank = &n
		}
		out.Benchmarks = append(out.Benchmarks, item)
	}

	writeJSON(w, http.StatusOK, out)
}

// aliasesOf 读一个模型的别名。读失败不把整页打成 503：别名是锦上添花，
// 档案与作品才是这一页的主体。
//
// 复用 `models.go` 里已有的 `modelAliases`（一次查全表再分组）：那个函数已经
// 在处理「目录列表每行都要别名」的场景，多一个「只查一个模型」的查询等于把
// 同一件事写成两份 SQL。
func (h *Handler) aliasesOf(ctx context.Context, modelID int64) []string {
	grouped, err := h.modelAliases(ctx)
	if err != nil {
		return []string{}
	}
	if v, ok := grouped[modelID]; ok {
		return v
	}
	return []string{}
}

// specsOf 把档案行摊成契约对象。缺值一律留 nil——界面显示「—」，
// 而不是显示一个看起来像真数据的 0。
func specsOf(s store.GetModelSpecsRow) *api.ModelSpecs {
	out := &api.ModelSpecs{
		ParamKind:     strPtrOrNil(s.ParamKind),
		ParamNote:     strPtrOrNil(s.ParamNote),
		ModalitiesIn:  strPtrOrNil(s.ModalitiesIn),
		ModalitiesOut: strPtrOrNil(s.ModalitiesOut),
		Source:        strPtrOrNil(s.Source),
		Note:          strPtrOrNil(s.Note),
	}
	if s.ContextWindow.Valid {
		n := int(s.ContextWindow.Int32)
		out.ContextWindow = &n
	}
	if s.MaxOutput.Valid {
		n := int(s.MaxOutput.Int32)
		out.MaxOutput = &n
	}
	if s.ParamTotalB.Valid {
		v := s.ParamTotalB.Float64
		out.ParamTotalB = &v
	}
	if s.ParamActiveB.Valid {
		v := s.ParamActiveB.Float64
		out.ParamActiveB = &v
	}
	if s.ReleaseDate.Valid {
		out.ReleaseDate = &openapi_types.Date{Time: s.ReleaseDate.Time}
	}
	if s.KnowledgeCutoff.Valid {
		v := s.KnowledgeCutoff.String
		out.KnowledgeCutoff = &v
	}
	if s.OpenWeights.Valid {
		n := int(s.OpenWeights.Int32)
		out.OpenWeights = &n
	}
	if s.CostIn.Valid {
		v := s.CostIn.Float64
		out.CostIn = &v
	}
	if s.CostOut.Valid {
		v := s.CostOut.Float64
		out.CostOut = &v
	}
	if s.License.Valid {
		v := s.License.String
		out.License = &v
	}
	if s.FetchedAt.Valid {
		t := s.FetchedAt.Time
		out.FetchedAt = &t
	}
	return out
}

// strPtrOrNil 把「空串表示没有」摊成指针。契约里这个字段 optional，
// 空串与 null 对读者没有区别，但 null 能让前端少写一次判断。
func strPtrOrNil(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}
