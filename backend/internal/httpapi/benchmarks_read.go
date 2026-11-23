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

// 总览矩阵的两个上限（#572）。
//
// 超限不静默截断：响应里带回截断前的总数，页面据此写「只显示前 N 列 / 前 N 行」。
// 它们不写成契约的 maxItems —— 那是「请求参数的校验」，客户端会照抄成候选值；
// 这里是服务端自己的陈列上限，改它不该让老调用方以为自己传错了参数。
const (
	benchmarkOverviewColumnLimit = 24
	benchmarkOverviewRowLimit    = 200
)

// GetBenchmarkOverview 对应 GET /api/v1/benchmarks/overview。
//
// 一条查询取回「列 + 格子」的扁平行，折成稀疏矩阵交给 foldBenchmarkOverview。
// 排序不在这里做第二遍：列序（能力组 → 榜名）写在 SQL 与契约的 description 里，
// 行序是折行后按可见格子数排的 —— SQL 是列主序吐的，排不了行序，所以只有这一处。
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
	writeJSON(w, http.StatusOK, foldBenchmarkOverview(rows))
}

// foldBenchmarkOverview 把扁平行折成稀疏矩阵，并应用 #572 的阅读规则。
//
// 三步的顺序不能换，换一步就是另一个矩阵：
//  1. 收列（SQL 已按「能力组 → 榜名」排好），按列上限截断 —— 被截掉的列不再有格子；
//  2. 收行与格子，只留可见列上的格子；一个可见格子都没有的模型不成行（否则会出现整行「—」）；
//  3. 行按可见格子数降序、再按模型名与 slug 升序，最后按行上限截断。
//
// 第 1 步先做，是为了让第 3 步的「覆盖数」与页面上看得见的列对得上：列被截掉之后
// 还按看不见的列排序，会把一批整行都是「—」的模型排到前面。
func foldBenchmarkOverview(flats []store.ListBenchmarkOverviewRow) api.BenchmarkOverview {
	columns := make([]api.BenchmarkOverviewColumn, 0, len(flats))
	seenColumn := make(map[string]bool, len(flats))
	for _, row := range flats {
		if seenColumn[row.BenchmarkSlug] {
			continue
		}
		seenColumn[row.BenchmarkSlug] = true
		columns = append(columns, api.BenchmarkOverviewColumn{
			BenchmarkSlug: row.BenchmarkSlug,
			Name:          row.BenchmarkName,
			Category:      api.BenchmarkOverviewColumnCategory(row.CategoryGroup),
			Unit:          row.Unit,
			Version:       row.Version,
		})
	}
	totalColumns := len(columns)
	if len(columns) > benchmarkOverviewColumnLimit {
		columns = columns[:benchmarkOverviewColumnLimit]
	}
	visible := make(map[string]bool, len(columns))
	for _, column := range columns {
		visible[column.BenchmarkSlug] = true
	}

	rows := make([]api.BenchmarkOverviewRow, 0, len(flats))
	rowAt := make(map[string]int, len(flats))
	for _, row := range flats {
		// LEFT JOIN 的空行：这张榜还没有匹配到站内模型的分数。列照常出现，格子不出现 ——
		// 缺格是「没测过」，不是「得了 0 分」。
		if !row.ModelSlug.Valid || !row.ModelName.Valid || !visible[row.BenchmarkSlug] {
			continue
		}
		index, seen := rowAt[row.ModelSlug.String]
		if !seen {
			index = len(rows)
			rowAt[row.ModelSlug.String] = index
			rows = append(rows, api.BenchmarkOverviewRow{
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
		rows[index].Cells = append(rows[index].Cells, cell)
	}

	sort.SliceStable(rows, func(i, j int) bool {
		if len(rows[i].Cells) != len(rows[j].Cells) {
			return len(rows[i].Cells) > len(rows[j].Cells)
		}
		if rows[i].ModelName != rows[j].ModelName {
			return rows[i].ModelName < rows[j].ModelName
		}
		return rows[i].ModelSlug < rows[j].ModelSlug
	})
	totalRows := len(rows)
	if len(rows) > benchmarkOverviewRowLimit {
		rows = rows[:benchmarkOverviewRowLimit]
	}

	return api.BenchmarkOverview{
		Columns:      columns,
		Rows:         rows,
		TotalColumns: totalColumns,
		TotalRows:    totalRows,
	}
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
