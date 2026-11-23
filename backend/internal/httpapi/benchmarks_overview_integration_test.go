package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
)

// 模型 × 基准总览（#571）。
//
// 这条用例钉的是**口径**，不是渲染：口径只要有一处写岔，矩阵就会悄悄多一列或少一格，
// 而页面看起来完全正常。四个反例各自对应契约里的一句：
//   - `cite` 档不进矩阵（列不出现）；
//   - 没有主指标的榜不进矩阵（列不出现）；
//   - 版本取最新**非自报**（列头 version 是 v2，不是更新的 self_reported v3）；
//   - 分数只取 matched / ranked / model_id 非空（未匹配的外部名、未排名次的行都不成行）。

func seedBenchmarkOverview(t *testing.T, h *Handler) {
	t.Helper()
	ctx := context.Background()
	cleanupBenchmarkOverview(t, h)

	mustExec := func(sql string, args ...any) {
		t.Helper()
		if _, err := h.pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("准备总览数据失败: %v\nSQL: %s", err, sql)
		}
	}

	mustExec(`INSERT INTO models (slug, name, status) VALUES
		('it-model-ov-a', 'Zeta Model', 'active'),
		('it-model-ov-b', 'Alpha Model', 'active'),
		('it-model-ov-c', 'Beta Model', 'active')`)

	mustExec(`INSERT INTO benchmarks (slug, name, display_tier) VALUES
		('it-bench-ov-table', 'B 总览榜', 'table'),
		('it-bench-ov-empty', 'A 空列榜', 'table'),
		('it-bench-ov-cite', 'C 引用榜', 'cite'),
		('it-bench-ov-noprimary', 'D 无主指标', 'table')`)

	// 三版：v1 旧、v2 当前、v3 自报且更新 —— 取 v2。
	mustExec(`INSERT INTO benchmark_versions (benchmark_id, version, as_of, source_type, coverage)
		SELECT id, v.version, v.as_of::date, v.source_type, 'full'
		FROM benchmarks b,
		     (VALUES ('v1', '2026-11-01', 'leaderboard'),
		             ('v2', '2026-11-10', 'leaderboard'),
		             ('v3', '2026-11-20', 'self_reported')) AS v(version, as_of, source_type)
		WHERE b.slug = 'it-bench-ov-table'`)
	mustExec(`INSERT INTO benchmark_versions (benchmark_id, version, as_of, source_type)
		SELECT id, 'v1', '2026-11-05'::date, 'leaderboard' FROM benchmarks WHERE slug = 'it-bench-ov-empty'`)
	mustExec(`INSERT INTO benchmark_versions (benchmark_id, version, as_of, source_type)
		SELECT id, 'v1', '2026-11-05'::date, 'leaderboard' FROM benchmarks WHERE slug = 'it-bench-ov-cite'`)
	mustExec(`INSERT INTO benchmark_versions (benchmark_id, version, as_of, source_type)
		SELECT id, 'v1', '2026-11-05'::date, 'leaderboard' FROM benchmarks WHERE slug = 'it-bench-ov-noprimary'`)

	// v2 的主指标 + 一个**非主指标**（后者有分数也不该进矩阵）；v1 也放一个主指标，用来验版本选择。
	mustExec(`INSERT INTO benchmark_metrics (version_id, key, label, unit, is_primary, sort)
		SELECT bv.id, m.key, m.label, m.unit, m.is_primary, m.sort
		FROM benchmark_versions bv
		  JOIN benchmarks b ON b.id = bv.benchmark_id
		  CROSS JOIN (VALUES ('score', '得分', '%', true, 0),
		                     ('cost', '单次成本', 'USD', false, 1)) AS m(key, label, unit, is_primary, sort)
		WHERE b.slug = 'it-bench-ov-table' AND bv.version = 'v2'`)
	mustExec(`INSERT INTO benchmark_metrics (version_id, key, label, unit, is_primary)
		SELECT bv.id, 'score', '得分', '%', true
		FROM benchmark_versions bv JOIN benchmarks b ON b.id = bv.benchmark_id
		WHERE (b.slug = 'it-bench-ov-table' AND bv.version = 'v1')
		   OR b.slug IN ('it-bench-ov-empty', 'it-bench-ov-cite')`)
	mustExec(`INSERT INTO benchmark_metrics (version_id, key, label, unit, is_primary)
		SELECT bv.id, 'score', '得分', '%', false
		FROM benchmark_versions bv JOIN benchmarks b ON b.id = bv.benchmark_id
		WHERE b.slug = 'it-bench-ov-noprimary'`)

	// 主指标分数：Zeta 有名次、Alpha 无 名次（rank 可空）、Beta 未排名次（不成行）、
	// 一条未匹配的外部名（不成行）；另外 v1 主指标、v3 自报主指标、非主指标各留一条，
	// 只要取错了它们就会多出格子或换掉列头版本。
	mustExec(`INSERT INTO benchmark_scores (metric_id, model_id, external_name, external_name_key,
	                                       match_status, raw_score, rank, ranked)
		SELECT bm.id, m.id, m.name, lower(m.name), 'matched', s.raw, s.rank, s.ranked
		FROM benchmark_versions bv
		  JOIN benchmarks b ON b.id = bv.benchmark_id
		  JOIN benchmark_metrics bm ON bm.version_id = bv.id AND bm.key = 'score' AND bm.is_primary
		  CROSS JOIN (VALUES ('it-model-ov-a', 81.0, 1, true),
		                     ('it-model-ov-b', 74.5, NULL::integer, true),
		                     ('it-model-ov-c', 70.0, 3, false)) AS s(slug, raw, rank, ranked)
		  JOIN models m ON m.slug = s.slug
		WHERE b.slug = 'it-bench-ov-table' AND bv.version = 'v2'`)
	mustExec(`INSERT INTO benchmark_scores (metric_id, model_id, external_name, external_name_key,
	                                       match_status, raw_score, rank)
		SELECT bm.id, NULL, 'Ghost Model', 'ghostmodel', 'unmatched', 99.0, 0
		FROM benchmark_versions bv JOIN benchmarks b ON b.id = bv.benchmark_id
		  JOIN benchmark_metrics bm ON bm.version_id = bv.id AND bm.is_primary
		WHERE b.slug = 'it-bench-ov-table' AND bv.version = 'v2'`)
	mustExec(`INSERT INTO benchmark_scores (metric_id, model_id, external_name, external_name_key,
	                                       match_status, raw_score, rank)
		SELECT bm.id, m.id, m.name, lower(m.name), 'matched', 0.42, NULL
		FROM benchmark_versions bv JOIN benchmarks b ON b.id = bv.benchmark_id
		  JOIN benchmark_metrics bm ON bm.version_id = bv.id AND bm.key = 'cost'
		  JOIN models m ON m.slug = 'it-model-ov-a'
		WHERE b.slug = 'it-bench-ov-table' AND bv.version = 'v2'`)
	mustExec(`INSERT INTO benchmark_scores (metric_id, model_id, external_name, external_name_key,
	                                       match_status, raw_score, rank)
		SELECT bm.id, m.id, m.name, lower(m.name), 'matched', 99.0, 1
		FROM benchmark_versions bv JOIN benchmarks b ON b.id = bv.benchmark_id
		  JOIN benchmark_metrics bm ON bm.version_id = bv.id AND bm.is_primary
		  JOIN models m ON m.slug = 'it-model-ov-a'
		WHERE (b.slug = 'it-bench-ov-table' AND bv.version IN ('v1', 'v3'))
		   OR b.slug = 'it-bench-ov-cite'`)
}

func cleanupBenchmarkOverview(t *testing.T, h *Handler) {
	t.Helper()
	ctx := context.Background()
	slugs := `('it-bench-ov-table','it-bench-ov-empty','it-bench-ov-cite','it-bench-ov-noprimary')`
	_, _ = h.pool.Exec(ctx, `DELETE FROM benchmark_scores WHERE metric_id IN (
        SELECT bm.id FROM benchmark_metrics bm
          JOIN benchmark_versions bv ON bv.id = bm.version_id
          JOIN benchmarks b ON b.id = bv.benchmark_id WHERE b.slug IN `+slugs+`)`)
	_, _ = h.pool.Exec(ctx, `DELETE FROM benchmark_metrics WHERE version_id IN (
        SELECT bv.id FROM benchmark_versions bv JOIN benchmarks b ON b.id = bv.benchmark_id
         WHERE b.slug IN `+slugs+`)`)
	_, _ = h.pool.Exec(ctx, `DELETE FROM benchmark_versions WHERE benchmark_id IN (
        SELECT id FROM benchmarks WHERE slug IN `+slugs+`)`)
	_, _ = h.pool.Exec(ctx, `DELETE FROM benchmarks WHERE slug IN `+slugs+`)`)
	_, _ = h.pool.Exec(ctx, `DELETE FROM models WHERE slug IN
        ('it-model-ov-a','it-model-ov-b','it-model-ov-c')`)
}

func TestIntegrationBenchmarkOverview(t *testing.T) {
	h, _, pool := newSortHandler(t)
	h = h.WithPool(pool)
	seedBenchmarkOverview(t, h)
	t.Cleanup(func() { cleanupBenchmarkOverview(t, h) })

	rec := httptest.NewRecorder()
	h.GetBenchmarkOverview(rec, httptest.NewRequest(http.MethodGet, "/api/v1/benchmarks/overview", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("总览 = %d，想要 200：%s", rec.Code, rec.Body.String())
	}
	var body api.BenchmarkOverview
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应不是合法 JSON: %v", err)
	}
	// 空矩阵要序列化成 [] 而不是 null —— 页面靠它区分「空态」与「加载失败」。
	if body.Columns == nil || body.Rows == nil {
		t.Fatalf("两个数组必须是 [] 而不是 null：columns=%v rows=%v", body.Columns, body.Rows)
	}

	column := map[string]api.BenchmarkOverviewColumn{}
	for _, c := range body.Columns {
		column[c.BenchmarkSlug] = c
	}
	if _, ok := column["it-bench-ov-cite"]; ok {
		t.Fatal("cite 档的榜进了矩阵：没有分数可展示的榜不该占一列")
	}
	if _, ok := column["it-bench-ov-noprimary"]; ok {
		t.Fatal("没有主指标的榜进了矩阵：一列只能放一个数")
	}
	if _, ok := column["it-bench-ov-empty"]; !ok {
		t.Fatal("有主指标但一条分数都没有的榜掉出了矩阵：列头要照常出现")
	}
	table, ok := column["it-bench-ov-table"]
	if !ok {
		t.Fatal("table 档的榜没进矩阵")
	}
	if table.Version != "v2" {
		t.Fatalf("列头版本 = %q，想要 v2（最新非自报版本）：自报版本不该顶掉第三方版本", table.Version)
	}
	if table.Unit != "%" {
		t.Fatalf("列头单位 = %q，想要 %%（主指标的单位）", table.Unit)
	}

	// 行：Zeta(有名次) 与 Alpha(无名次)；Beta 未排名次、Ghost 未匹配，都不该成为一行。
	names := make([]string, 0, len(body.Rows))
	rows := map[string]api.BenchmarkOverviewRow{}
	for _, row := range body.Rows {
		names = append(names, row.ModelName)
		rows[row.ModelSlug] = row
	}
	if _, ok := rows["it-model-ov-c"]; ok {
		t.Fatal("ranked=false 的分数成了行：矩阵只收有排名的分")
	}
	for _, name := range names {
		if name == "Ghost Model" {
			t.Fatal("未匹配的外部名成了行：它没有 model_id，页面上带不出模型链接")
		}
	}
	if len(names) < 2 || names[0] != "Alpha Model" || names[1] != "Zeta Model" {
		t.Fatalf("行没有按模型名升序：%v", names)
	}

	zeta, ok := rows["it-model-ov-a"]
	if !ok {
		t.Fatal("Zeta 不在行里")
	}
	// 稀疏 + 非主指标不进格子：该列只应有 1 个格子（主指标），不能多出 cost 那一格。
	if len(zeta.Cells) != 1 {
		t.Fatalf("Zeta 的格子 = %+v，想要 1 个（非主指标的分数不该进矩阵）", zeta.Cells)
	}
	if zeta.Cells[0].BenchmarkSlug != "it-bench-ov-table" || zeta.Cells[0].RawScore != 81 {
		t.Fatalf("Zeta 的格子内容不对：%+v", zeta.Cells[0])
	}
	if zeta.Cells[0].Rank == nil || *zeta.Cells[0].Rank != 1 {
		t.Fatalf("Zeta 的名次 = %v，想要 1", zeta.Cells[0].Rank)
	}

	alpha := rows["it-model-ov-b"]
	if len(alpha.Cells) != 1 {
		t.Fatalf("Alpha 的格子 = %+v，想要 1 个：空列不该补零", alpha.Cells)
	}
	if alpha.Cells[0].Rank != nil {
		t.Fatalf("Alpha 的名次 = %v，想要 nil（源榜没给名次）", *alpha.Cells[0].Rank)
	}
}
