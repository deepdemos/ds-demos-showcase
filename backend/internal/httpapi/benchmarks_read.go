package httpapi

import (
	"context"
	"errors"
	"net/http"
	"sort"

	"github.com/jackc/pgx/v5"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// 第三方基准的读路径（#427 / 模型域 §5.6）。
//
// 这一层只做**分榜展示**：每张榜的原始分与名次各自陈列，跨榜不比较、不合成。
// 归一化（§5.3）与综合分（§5.4）还没实现 —— 所以这里不返回任何「总分」字段，
// 也不留占位：契约里没有的东西，前端就编不出来。

// ListBenchmarks 对应 GET /api/v1/benchmarks。
func (h *Handler) ListBenchmarks(w http.ResponseWriter, r *http.Request) {
	if h.queries == nil {
		unavailable(w, errors.New("database not configured"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), queryTimeout)
	defer cancel()

	rows, err := h.queries.ListBenchmarks(ctx)
	if err != nil {
		unavailable(w, err)
		return
	}
	items := make([]api.BenchmarkSummary, 0, len(rows))
	for _, row := range rows {
		item := api.BenchmarkSummary{
			Slug:        row.Slug,
			Name:        row.Name,
			Publisher:   row.Publisher,
			Category:    row.Category,
			DisplayTier: api.BenchmarkSummaryDisplayTier(row.DisplayTier),
		}
		if row.LatestAsOf.Valid {
			asOf := openapi_types.Date{Time: row.LatestAsOf.Time}
			item.LatestAsOf = &asOf
		}
		items = append(items, item)
	}
	writeJSON(w, http.StatusOK, api.BenchmarkList{Items: items})
}

// GetBenchmarkOverview 对应 GET /api/v1/benchmarks/overview。
//
// 一条查询取回「列 + 格子」的扁平行，在这里折成稀疏矩阵。列的先后按 SQL 给的榜名升序；
// 行按模型名在这里排一次 —— SQL 是列主序吐的（同一列的所有模型连着出），不排就是乱的。
// 除这一处之外不再排序：排序规则只有一份，写在查询与契约的 description 里。
func (h *Handler) GetBenchmarkOverview(w http.ResponseWriter, r *http.Request) {
	if h.queries == nil {
		unavailable(w, errors.New("database not configured"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), queryTimeout)
	defer cancel()

	rows, err := h.queries.ListBenchmarkOverview(ctx)
	if err != nil {
		unavailable(w, err)
		return
	}

	out := api.BenchmarkOverview{
		Columns: []api.BenchmarkOverviewColumn{},
		Rows:    []api.BenchmarkOverviewRow{},
	}
	columnAt := make(map[string]int, len(rows))
	rowAt := make(map[string]int, len(rows))
	for _, row := range rows {
		if _, seen := columnAt[row.BenchmarkSlug]; !seen {
			columnAt[row.BenchmarkSlug] = len(out.Columns)
			out.Columns = append(out.Columns, api.BenchmarkOverviewColumn{
				BenchmarkSlug: row.BenchmarkSlug,
				Name:          row.BenchmarkName,
				Unit:          row.Unit,
				Version:       row.Version,
			})
		}
		// LEFT JOIN 的空行：这张榜还没有匹配到站内模型的分数。列照常出现，格子不出现 ——
		// 缺格是「没测过」，不是「得了 0 分」。
		if !row.ModelSlug.Valid || !row.ModelName.Valid {
			continue
		}
		index, seen := rowAt[row.ModelSlug.String]
		if !seen {
			index = len(out.Rows)
			rowAt[row.ModelSlug.String] = index
			out.Rows = append(out.Rows, api.BenchmarkOverviewRow{
				ModelSlug: row.ModelSlug.String,
				ModelName: row.ModelName.String,
				Cells:     []api.BenchmarkOverviewCell{},
			})
		}
		cell := api.BenchmarkOverviewCell{
			BenchmarkSlug: row.BenchmarkSlug,
			RawScore:      row.RawScore.Float64,
		}
		if row.Rank.Valid {
			rank := int(row.Rank.Int32)
			cell.Rank = &rank
		}
		out.Rows[index].Cells = append(out.Rows[index].Cells, cell)
	}

	sort.SliceStable(out.Rows, func(i, j int) bool {
		if out.Rows[i].ModelName != out.Rows[j].ModelName {
			return out.Rows[i].ModelName < out.Rows[j].ModelName
		}
		return out.Rows[i].ModelSlug < out.Rows[j].ModelSlug
	})

	writeJSON(w, http.StatusOK, out)
}

// GetBenchmark 对应 GET /api/v1/benchmarks/{slug}。
func (h *Handler) GetBenchmark(w http.ResponseWriter, r *http.Request, slug string) {
	if h.queries == nil {
		unavailable(w, errors.New("database not configured"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), queryTimeout)
	defer cancel()

	row, err := h.queries.GetBenchmarkBySlug(ctx, slug)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, api.NotFound, "这张基准不存在或已下架")
		return
	}
	if err != nil {
		unavailable(w, err)
		return
	}

	versions, err := h.queries.ListBenchmarkVersions(ctx, row.ID)
	if err != nil {
		unavailable(w, err)
		return
	}

	out := api.BenchmarkDetail{
		Slug:        row.Slug,
		Name:        row.Name,
		Publisher:   row.Publisher,
		HomepageUrl: row.HomepageUrl,
		Category:    row.Category,
		DisplayTier: api.BenchmarkDetailDisplayTier(row.DisplayTier),
		License:     &row.License,
		LicenseUrl:  &row.LicenseUrl,
		Attribution: &row.Attribution,
	}
	if row.Description != "" {
		description := row.Description
		out.Description = &description
	}

	out.Versions = make([]api.BenchmarkVersionSummary, 0, len(versions))
	var latestVersionID int64
	for _, v := range versions {
		item := api.BenchmarkVersionSummary{
			Version:    v.Version,
			AsOf:       openapi_types.Date{Time: v.AsOf.Time},
			SourceType: v.SourceType,
			Coverage:   v.Coverage,
		}
		sourceURL := v.SourceUrl
		item.SourceUrl = &sourceURL
		if v.ParticipantCount.Valid {
			count := int(v.ParticipantCount.Int64)
			item.ParticipantCount = &count
		}
		out.Versions = append(out.Versions, item)

		// 指标表只取**最新一个非自报版本**：厂商自报与第三方测评是两种凭据（MODEL-020），
		// 混进同一张表会让读者以为它们可比。
		if latestVersionID == 0 && v.SourceType != "self_reported" {
			latestVersionID = v.ID
		}
	}

	out.Metrics = []api.BenchmarkMetricRow{}
	if latestVersionID != 0 {
		rows, err := h.queries.ListBenchmarkMetricsWithScores(ctx, latestVersionID)
		if err != nil {
			unavailable(w, err)
			return
		}
		out.Metrics = groupBenchmarkMetrics(rows)
	}

	writeJSON(w, http.StatusOK, out)
}

// groupBenchmarkMetrics 把「指标 × 分数」的扁平行折成指标数组。
//
// 依赖 SQL 的排序（主榜在前、再按 sort、最后按名次）—— 这里只在 key 变化时开新组，
// 不做二次排序：两处各排一次，迟早给出两种顺序。
func groupBenchmarkMetrics(rows []store.ListBenchmarkMetricsWithScoresRow) []api.BenchmarkMetricRow {
	out := make([]api.BenchmarkMetricRow, 0, len(rows))
	for _, row := range rows {
		if len(out) == 0 || out[len(out)-1].Key != row.Key {
			out = append(out, api.BenchmarkMetricRow{
				Key:            row.Key,
				Label:          row.Label,
				Unit:           row.Unit,
				IsPrimary:      row.IsPrimary,
				HigherIsBetter: row.HigherIsBetter,
				Scores:         []api.BenchmarkScoreRow{},
			})
		}
		// LEFT JOIN 的空行：指标存在、一条分数都没有。空榜是正常状态，照常返回指标。
		if !row.ExternalName.Valid {
			continue
		}
		score := api.BenchmarkScoreRow{
			ExternalName: row.ExternalName.String,
			RawScore:     row.RawScore.Float64,
			MatchStatus:  row.MatchStatus.String,
		}
		if row.ModelSlug.Valid {
			slug := row.ModelSlug.String
			score.ModelSlug = &slug
		}
		if row.ModelName.Valid {
			name := row.ModelName.String
			score.ModelName = &name
		}
		if row.Rank.Valid {
			rank := int(row.Rank.Int32)
			score.Rank = &rank
		}
		if row.SampleSize.Valid {
			size := int(row.SampleSize.Int64)
			score.SampleSize = &size
		}
		if row.UncertaintyLower.Valid {
			lower := row.UncertaintyLower.Float64
			score.UncertaintyLower = &lower
		}
		if row.UncertaintyUpper.Valid {
			upper := row.UncertaintyUpper.Float64
			score.UncertaintyUpper = &upper
		}
		last := &out[len(out)-1]
		last.Scores = append(last.Scores, score)
	}
	return out
}
