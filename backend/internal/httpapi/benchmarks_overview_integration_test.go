package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
)

// 模型 × 基准总览（#571 / #572）。
//
// 这条用例钉的是**口径与阅读规则**，不是渲染：口径只要有一处写岔，矩阵就会悄悄多一列
// 或少一格，而页面看起来完全正常。四个反例各自对应契约里的一句：
//   - `cite` 档不进矩阵（列不出现）；
//   - 没有主指标的榜不进矩阵（列不出现）；
//   - 版本取最新**非自报**（列头 version 是 v2，不是更新的 self_reported v3）；
//   - 分数只取 matched / ranked / model_id 非空（未匹配的外部名、未排名次的行都不成行）。
//
// #572 的三条阅读规则也在这条用例里：列按能力组（coding → agent → math）而不是榜名排、
// 行按覆盖列数降序再按模型名、两个 total 是截断前的总数（本用例没有超限，所以等于长度）。

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
		('it-model-ov-c', 'Beta Model', 'active'),
		('it-model-ov-d', 'Gamma Model', 'active')`)

	// 能力组：table 是 coding、second 是 agent、empty 是「math,agent」（多值取第一个值，
	// 即 math）。列序因此是 coding → agent → math，而按榜名排会是 A → B → E —— 两者不同，
	// 排序写错这条用例才会红。
	mustExec(`INSERT INTO benchmarks (slug, name, category, display_tier) VALUES
		('it-bench-ov-table', 'B 总览榜', 'coding', 'table'),
		('it-bench-ov-empty', 'A 空列榜', 'math,agent', 'table'),
		('it-bench-ov-cite', 'C 引用榜', 'general', 'cite'),
		('it-bench-ov-noprimary', 'D 无主指标', 'general', 'table'),
		('it-bench-ov-second', 'E 第二榜', 'agent', 'table')`)

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
	mustExec(`INSERT INTO benchmark_versions (benchmark_id, version, as_of, source_type)
		SELECT id, 'v1', '2026-11-05'::date, 'leaderboard' FROM benchmarks WHERE slug = 'it-bench-ov-second'`)

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
	mustExec(`INSERT INTO benchmark_metrics (version_id, key, label, unit, is_primary)
		SELECT bv.id, 'score', '得分', '%', true
		FROM benchmark_versions bv JOIN benchmarks b ON b.id = bv.benchmark_id
		WHERE b.slug = 'it-bench-ov-second'`)

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
	// 第二榜：Zeta 与 Gamma 各一格。Zeta 因此覆盖 2 列（table + second），Alpha 只覆盖 table 一列；
	// 按覆盖数排是 Zeta 在前，按模型名排是 Alpha 在前 —— 两者不同，行序写错这条用例才会红。
	// Alpha 与 Gamma 覆盖数并列，靠模型名决定先后（Alpha < Gamma）。
	mustExec(`INSERT INTO benchmark_scores (metric_id, model_id, external_name, external_name_key,
	                                       match_status, raw_score, rank, ranked)
		SELECT bm.id, m.id, m.name, lower(m.name), 'matched', s.raw, s.rank, true
		FROM benchmark_versions bv
		  JOIN benchmarks b ON b.id = bv.benchmark_id
		  JOIN benchmark_metrics bm ON bm.version_id = bv.id AND bm.is_primary
		  CROSS JOIN (VALUES ('it-model-ov-a', 74.0, 2),
		                     ('it-model-ov-d', 66.0, 3)) AS s(slug, raw, rank)
		  JOIN models m ON m.slug = s.slug
		WHERE b.slug = 'it-bench-ov-second' AND bv.version = 'v1'`)
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

var (
	benchmarkOverviewSlugs = []string{
		"it-bench-ov-table",
		"it-bench-ov-empty",
		"it-bench-ov-cite",
		"it-bench-ov-noprimary",
		"it-bench-ov-second",
	}
	benchmarkOverviewModelSlugs = []string{
		"it-model-ov-a",
		"it-model-ov-b",
		"it-model-ov-c",
		"it-model-ov-d",
	}
)

type benchmarkOverviewCleanupStmt struct {
	sql  string
	args []any
}

// benchmarkOverviewCleanupStmts 按外键依赖逆序清理全部总览测试数据，使用参数化 ANY($1)
// 避免手拼括号引发类似 #600 的语法错误。
func benchmarkOverviewCleanupStmts() []benchmarkOverviewCleanupStmt {
	return []benchmarkOverviewCleanupStmt{
		{
			sql: `DELETE FROM benchmark_scores WHERE metric_id IN (
        SELECT bm.id FROM benchmark_metrics bm
          JOIN benchmark_versions bv ON bv.id = bm.version_id
          JOIN benchmarks b ON b.id = bv.benchmark_id WHERE b.slug = ANY($1))`,
			args: []any{benchmarkOverviewSlugs},
		},
		{
			sql: `DELETE FROM benchmark_metrics WHERE version_id IN (
        SELECT bv.id FROM benchmark_versions bv JOIN benchmarks b ON b.id = bv.benchmark_id
         WHERE b.slug = ANY($1))`,
			args: []any{benchmarkOverviewSlugs},
		},
		{
			sql: `DELETE FROM benchmark_versions WHERE benchmark_id IN (
        SELECT id FROM benchmarks WHERE slug = ANY($1))`,
			args: []any{benchmarkOverviewSlugs},
		},
		{
			sql:  `DELETE FROM benchmarks WHERE slug = ANY($1)`,
			args: []any{benchmarkOverviewSlugs},
		},
		{
			sql:  `DELETE FROM models WHERE slug = ANY($1)`,
			args: []any{benchmarkOverviewModelSlugs},
		},
	}
}

// runBenchmarkOverviewCleanup 执行全部清理语句并收集每条错误，避免单条报错被静默吞掉
// 或中断后续表的清理。
func runBenchmarkOverviewCleanup(ctx context.Context, exec func(context.Context, string, ...any) error) []error {
	var errs []error
	for _, stmt := range benchmarkOverviewCleanupStmts() {
		if err := exec(ctx, stmt.sql, stmt.args...); err != nil {
			errs = append(errs, fmt.Errorf("清理总览数据失败: %w\nSQL: %s", err, stmt.sql))
		}
	}
	return errs
}

func cleanupBenchmarkOverview(t *testing.T, h *Handler) {
	t.Helper()
	ctx := context.Background()
	errs := runBenchmarkOverviewCleanup(ctx, func(ctx context.Context, sql string, args ...any) error {
		_, err := h.pool.Exec(ctx, sql, args...)
		return err
	})
	for _, err := range errs {
		t.Error(err)
	}
	if len(errs) > 0 {
		t.FailNow()
	}
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
	// 列序按能力组，不按榜名：按榜名会是 A 空列榜 → B 总览榜 → E 第二榜。
	wantColumns := []struct {
		slug     string
		category api.BenchmarkOverviewColumnCategory
	}{
		{"it-bench-ov-table", api.Coding},
		{"it-bench-ov-second", api.Agent},
		// 多值「math,agent」取第一个值，即 math。
		{"it-bench-ov-empty", api.Math},
	}
	if len(body.Columns) != len(wantColumns) {
		t.Fatalf("列数 = %d，想要 %d：%+v", len(body.Columns), len(wantColumns), body.Columns)
	}
	for i, want := range wantColumns {
		if body.Columns[i].BenchmarkSlug != want.slug {
			t.Fatalf("第 %d 列 = %q，想要 %q：列要先按能力组（coding → agent → general → math → other）再按榜名",
				i, body.Columns[i].BenchmarkSlug, want.slug)
		}
		if body.Columns[i].Category != want.category {
			t.Fatalf("第 %d 列的能力组 = %q，想要 %q", i, body.Columns[i].Category, want.category)
		}
	}
	if body.TotalColumns != len(body.Columns) || body.TotalRows != len(body.Rows) {
		t.Fatalf("没有超限时两个 total 应等于长度：totalColumns=%d/%d totalRows=%d/%d",
			body.TotalColumns, len(body.Columns), body.TotalRows, len(body.Rows))
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

	// 行：Zeta 覆盖 2 列（table + second）排第一；Alpha 与 Gamma 都只覆盖 1 列，按模型名排
	//（Alpha < Gamma）。整体按模型名排会得到 Alpha → Gamma → Zeta —— 与本例不同。
	// Beta 未排名次、Ghost 未匹配，都不该成为一行。
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
	wantNames := []string{"Zeta Model", "Alpha Model", "Gamma Model"}
	if len(names) != len(wantNames) {
		t.Fatalf("行数 = %d，想要 %d：%v", len(names), len(wantNames), names)
	}
	for i, want := range wantNames {
		if names[i] != want {
			t.Fatalf("第 %d 行 = %q，想要 %q：行要先按覆盖列数降序、再按模型名（%v）", i, names[i], want, names)
		}
	}

	zeta, ok := rows["it-model-ov-a"]
	if !ok {
		t.Fatal("Zeta 不在行里")
	}
	// 稀疏 + 非主指标不进格子：table 榜只有主指标那一格，不能多出 cost 那一格；
	// second 榜那一格来自另一个主指标。格子按列序出现（table → second）。
	if len(zeta.Cells) != 2 {
		t.Fatalf("Zeta 的格子 = %+v，想要 2 个（table + second；非主指标的分数不该进矩阵）", zeta.Cells)
	}
	if zeta.Cells[0].BenchmarkSlug != "it-bench-ov-table" || zeta.Cells[0].RawScore != 81 {
		t.Fatalf("Zeta 的第一个格子内容不对：%+v", zeta.Cells[0])
	}
	if zeta.Cells[0].Rank == nil || *zeta.Cells[0].Rank != 1 {
		t.Fatalf("Zeta 在 table 榜的名次 = %v，想要 1", zeta.Cells[0].Rank)
	}
	if zeta.Cells[1].BenchmarkSlug != "it-bench-ov-second" || zeta.Cells[1].RawScore != 74 {
		t.Fatalf("Zeta 的第二个格子内容不对：%+v", zeta.Cells[1])
	}

	alpha := rows["it-model-ov-b"]
	if len(alpha.Cells) != 1 {
		t.Fatalf("Alpha 的格子 = %+v，想要 1 个（只有 table 榜）：空列不该补零", alpha.Cells)
	}
	if alpha.Cells[0].Rank != nil {
		t.Fatalf("Alpha 在 table 榜的名次 = %v，想要 nil（源榜没给名次）", alpha.Cells[0].Rank)
	}

	gamma := rows["it-model-ov-d"]
	if len(gamma.Cells) != 1 || gamma.Cells[0].BenchmarkSlug != "it-bench-ov-second" {
		t.Fatalf("Gamma 的格子 = %+v，想要 1 个（只有 second 榜）", gamma.Cells)
	}
	if gamma.Cells[0].Rank == nil || *gamma.Cells[0].Rank != 3 {
		t.Fatalf("Gamma 在 second 榜的名次 = %v，想要 3", gamma.Cells[0].Rank)
	}
}

// 回归测试（#600）：清理语句不再静默丢弃错误，且单条报错后仍会跑完剩余表。
func TestBenchmarkOverviewCleanupReportsErrors(t *testing.T) {
	wantTables := []string{
		"DELETE FROM benchmark_scores",
		"DELETE FROM benchmark_metrics",
		"DELETE FROM benchmark_versions",
		"DELETE FROM benchmarks",
		"DELETE FROM models",
	}

	var executed []string
	if errs := runBenchmarkOverviewCleanup(context.Background(), func(_ context.Context, sql string, args ...any) error {
		executed = append(executed, sql)
		if len(args) != 1 {
			t.Fatalf("清理语句参数个数 = %d，想要 1：%s", len(args), sql)
		}
		gotSlugs, ok := args[0].([]string)
		if !ok {
			t.Fatalf("清理语句参数类型 = %T，想要 []string：%s", args[0], sql)
		}
		wantSlugs := benchmarkOverviewSlugs
		if strings.HasPrefix(sql, "DELETE FROM models") {
			wantSlugs = benchmarkOverviewModelSlugs
		}
		if !slices.Equal(gotSlugs, wantSlugs) {
			t.Fatalf("清理语句 slug 参数 = %v，想要 %v", gotSlugs, wantSlugs)
		}
		return nil
	}); len(errs) != 0 {
		t.Fatalf("无错误时应返回空错误列表，实际 = %v", errs)
	}
	if len(executed) != len(wantTables) {
		t.Fatalf("执行语句数 = %d，想要 %d", len(executed), len(wantTables))
	}
	for i, prefix := range wantTables {
		if !strings.HasPrefix(executed[i], prefix) {
			t.Fatalf("第 %d 条清理语句 = %q，想要以 %q 开头", i, executed[i], prefix)
		}
	}

	// 模拟 #600 事故现场：DELETE FROM benchmarks 报语法错时不得被吞，且 models 清理仍继续执行。
	wantErr := errors.New(`ERROR: syntax error at or near ")" (SQLSTATE 42601)`)
	called := 0
	errs := runBenchmarkOverviewCleanup(context.Background(), func(_ context.Context, sql string, _ ...any) error {
		called++
		if strings.HasPrefix(sql, "DELETE FROM benchmarks ") {
			return wantErr
		}
		return nil
	})
	if called != len(wantTables) {
		t.Fatalf("中间语句出错后仍应执行完全部 %d 条清理语句，实际执行 %d 条", len(wantTables), called)
	}
	if len(errs) != 1 || !errors.Is(errs[0], wantErr) {
		t.Fatalf("清理错误 = %v，想要包含 %v", errs, wantErr)
	}
}

// 回归测试（#600）：同一个测试库连续跑两轮 seed + cleanup，必须零报错、零残留，
// 防止清理失败只在本地复用库第二轮才暴露。
func TestIntegrationBenchmarkOverviewCleanupIsRepeatable(t *testing.T) {
	h, _, pool := newSortHandler(t)
	h = h.WithPool(pool)
	t.Cleanup(func() { cleanupBenchmarkOverview(t, h) })

	assertZero := func(round int) {
		t.Helper()
		ctx := context.Background()
		checks := []struct {
			table string
			sql   string
			slugs []string
		}{
			{
				table: "benchmark_scores",
				sql: `SELECT count(*) FROM benchmark_scores WHERE metric_id IN (
					SELECT bm.id FROM benchmark_metrics bm
					  JOIN benchmark_versions bv ON bv.id = bm.version_id
					  JOIN benchmarks b ON b.id = bv.benchmark_id WHERE b.slug = ANY($1))`,
				slugs: benchmarkOverviewSlugs,
			},
			{
				table: "benchmark_metrics",
				sql: `SELECT count(*) FROM benchmark_metrics WHERE version_id IN (
					SELECT bv.id FROM benchmark_versions bv
					  JOIN benchmarks b ON b.id = bv.benchmark_id WHERE b.slug = ANY($1))`,
				slugs: benchmarkOverviewSlugs,
			},
			{
				table: "benchmark_versions",
				sql: `SELECT count(*) FROM benchmark_versions WHERE benchmark_id IN (
					SELECT id FROM benchmarks WHERE slug = ANY($1))`,
				slugs: benchmarkOverviewSlugs,
			},
			{
				table: "benchmarks",
				sql:   `SELECT count(*) FROM benchmarks WHERE slug = ANY($1)`,
				slugs: benchmarkOverviewSlugs,
			},
			{
				table: "models",
				sql:   `SELECT count(*) FROM models WHERE slug = ANY($1)`,
				slugs: benchmarkOverviewModelSlugs,
			},
		}
		for _, c := range checks {
			var n int
			if err := h.pool.QueryRow(ctx, c.sql, c.slugs).Scan(&n); err != nil {
				t.Fatalf("第 %d 轮清理后查询 %s 残留失败: %v", round, c.table, err)
			}
			if n != 0 {
				t.Fatalf("第 %d 轮清理后 %s 仍残留 %d 行，想要 0", round, c.table, n)
			}
		}
	}

	for round := 1; round <= 2; round++ {
		seedBenchmarkOverview(t, h)
		cleanupBenchmarkOverview(t, h)
		assertZero(round)
	}
}
