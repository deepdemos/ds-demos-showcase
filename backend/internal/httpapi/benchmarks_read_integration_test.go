package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
)

// 第三方基准的读路径（#427 / 模型域 §5.6）。
//
// 钉三件事：列表只给五列且按名字排、详情把「指标 × 分数」折成指标数组、
// **空榜照常返回**（指标在、分数为空数组）—— 最后一条最容易实现成 404。

func seedBenchmarkRead(t *testing.T, h *Handler) {
	t.Helper()
	ctx := context.Background()
	cleanupBenchmarkRead(t, h)

	if _, err := h.pool.Exec(ctx, `
INSERT INTO benchmarks (slug, name, publisher, homepage_url, category, description, display_tier)
VALUES ('it-bench-read', 'IT 读路径榜', 'IT 发布方', 'https://it.example/bench', 'coding', '测一下读路径', 'table')`); err != nil {
		t.Fatalf("插入基准失败: %v", err)
	}
	// 第二张：cite 档、没有任何版本 —— 列表要能正常列出，且 latestAsOf 为 null。
	if _, err := h.pool.Exec(ctx, `
INSERT INTO benchmarks (slug, name, display_tier)
VALUES ('it-bench-empty', 'A 空榜', 'cite')`); err != nil {
		t.Fatalf("插入空榜失败: %v", err)
	}

	var versionID int64
	if err := h.pool.QueryRow(ctx, `
INSERT INTO benchmark_versions (benchmark_id, version, as_of, source_type, coverage, participant_count)
SELECT id, 'v1', '2026-11-01'::date, 'leaderboard', 'full', 12 FROM benchmarks WHERE slug = 'it-bench-read'
RETURNING id`).Scan(&versionID); err != nil {
		t.Fatalf("插入版本失败: %v", err)
	}

	var metricID int64
	if err := h.pool.QueryRow(ctx, `
INSERT INTO benchmark_metrics (version_id, key, label, is_primary, unit)
VALUES ($1, 'score', '得分', true, '%') RETURNING id`, versionID).Scan(&metricID); err != nil {
		t.Fatalf("插入指标失败: %v", err)
	}

	var modelID int64
	if err := h.pool.QueryRow(ctx, `
INSERT INTO models (slug, name, status) VALUES ('it-model-read', 'IT Model Read', 'active') RETURNING id`).Scan(&modelID); err != nil {
		t.Fatalf("插入模型失败: %v", err)
	}

	if _, err := h.pool.Exec(ctx, `
INSERT INTO benchmark_scores (metric_id, model_id, external_name, external_name_key, match_status,
                               raw_score, rank, sample_size, uncertainty_lower, uncertainty_upper)
VALUES ($1, $2, 'IT Model Read', 'itmodelread', 'matched', 42.5, 1, 200, 40, 45)`,
		metricID, modelID); err != nil {
		t.Fatalf("插入分数失败: %v", err)
	}
}

func cleanupBenchmarkRead(t *testing.T, h *Handler) {
	t.Helper()
	ctx := context.Background()
	_, _ = h.pool.Exec(ctx, `DELETE FROM benchmark_scores WHERE metric_id IN (
        SELECT bm.id FROM benchmark_metrics bm
          JOIN benchmark_versions bv ON bv.id = bm.version_id
          JOIN benchmarks b ON b.id = bv.benchmark_id WHERE b.slug IN ('it-bench-read','it-bench-empty'))`)
	_, _ = h.pool.Exec(ctx, `DELETE FROM benchmark_metrics WHERE version_id IN (
        SELECT bv.id FROM benchmark_versions bv JOIN benchmarks b ON b.id = bv.benchmark_id
         WHERE b.slug IN ('it-bench-read','it-bench-empty'))`)
	_, _ = h.pool.Exec(ctx, `DELETE FROM benchmark_versions WHERE benchmark_id IN (
        SELECT id FROM benchmarks WHERE slug IN ('it-bench-read','it-bench-empty'))`)
	_, _ = h.pool.Exec(ctx, `DELETE FROM benchmarks WHERE slug IN ('it-bench-read','it-bench-empty')`)
	_, _ = h.pool.Exec(ctx, `DELETE FROM models WHERE slug = 'it-model-read'`)
}

func TestIntegrationListBenchmarks(t *testing.T) {
	h, _, pool := newSortHandler(t)
	h = h.WithPool(pool)
	seedBenchmarkRead(t, h)
	t.Cleanup(func() { cleanupBenchmarkRead(t, h) })

	rec := httptest.NewRecorder()
	h.ListBenchmarks(rec, httptest.NewRequest(http.MethodGet, "/api/v1/benchmarks", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("列表 = %d，想要 200：%s", rec.Code, rec.Body.String())
	}
	var body api.BenchmarkList
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应不是合法 JSON: %v", err)
	}

	var read, empty *api.BenchmarkSummary
	for i := range body.Items {
		switch body.Items[i].Slug {
		case "it-bench-read":
			read = &body.Items[i]
		case "it-bench-empty":
			empty = &body.Items[i]
		}
	}
	if read == nil || empty == nil {
		t.Fatalf("两行没都出现：%+v", body.Items)
	}
	if read.Publisher != "IT 发布方" || read.Category != "coding" {
		t.Fatalf("列表五列内容不对：%+v", read)
	}
	if read.LatestAsOf == nil {
		t.Fatalf("有版本的榜 latestAsOf 为 nil")
	}
	if empty.LatestAsOf != nil {
		t.Fatalf("没版本的榜 latestAsOf = %v，想要 nil", empty.LatestAsOf)
	}
	if empty.DisplayTier != api.BenchmarkSummaryDisplayTier("cite") {
		t.Fatalf("空榜档位 = %q，想要 cite", empty.DisplayTier)
	}
	// 按名字升序：「A 空榜」在「IT 读路径榜」之前。
	if len(body.Items) >= 2 && body.Items[0].Name > body.Items[1].Name {
		t.Fatalf("没有按名字升序：%s > %s", body.Items[0].Name, body.Items[1].Name)
	}
}

func TestIntegrationGetBenchmarkDetail(t *testing.T) {
	h, _, pool := newSortHandler(t)
	h = h.WithPool(pool)
	seedBenchmarkRead(t, h)
	t.Cleanup(func() { cleanupBenchmarkRead(t, h) })

	rec := httptest.NewRecorder()
	h.GetBenchmark(rec, httptest.NewRequest(http.MethodGet, "/api/v1/benchmarks/it-bench-read", nil), "it-bench-read")
	if rec.Code != http.StatusOK {
		t.Fatalf("详情 = %d，想要 200：%s", rec.Code, rec.Body.String())
	}
	var body api.BenchmarkDetail
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应不是合法 JSON: %v", err)
	}
	if body.Slug != "it-bench-read" || body.License == nil {
		t.Fatalf("详情头部不对：%+v", body)
	}
	if len(body.Versions) != 1 || body.Versions[0].Version != "v1" {
		t.Fatalf("版本历史 = %+v，想要一条 v1", body.Versions)
	}
	if len(body.Metrics) != 1 || body.Metrics[0].Key != "score" {
		t.Fatalf("指标表 = %+v，想要一条 score", body.Metrics)
	}
	if len(body.Metrics[0].Scores) != 1 {
		t.Fatalf("分数 = %+v，想要一条", body.Metrics[0].Scores)
	}
	score := body.Metrics[0].Scores[0]
	if score.ModelSlug == nil || *score.ModelSlug != "it-model-read" {
		t.Fatalf("分数没带出站内模型：%+v", score)
	}
	if score.Rank == nil || *score.Rank != 1 {
		t.Fatalf("名次 = %v，想要 1", score.Rank)
	}
	if score.SampleSize == nil || *score.SampleSize != 200 {
		t.Fatalf("样本量 = %v，想要 200", score.SampleSize)
	}
	if score.UncertaintyLower == nil || *score.UncertaintyLower != 40 {
		t.Fatalf("区间下界 = %v，想要 40", score.UncertaintyLower)
	}
}

func TestIntegrationGetBenchmarkEmptyAndMissing(t *testing.T) {
	h, _, pool := newSortHandler(t)
	h = h.WithPool(pool)
	seedBenchmarkRead(t, h)
	t.Cleanup(func() { cleanupBenchmarkRead(t, h) })

	// 空榜：指标一条都没有，但 200 而不是 404 —— 空是正常状态。
	rec := httptest.NewRecorder()
	h.GetBenchmark(rec, httptest.NewRequest(http.MethodGet, "/api/v1/benchmarks/it-bench-empty", nil), "it-bench-empty")
	if rec.Code != http.StatusOK {
		t.Fatalf("空榜详情 = %d，想要 200：%s", rec.Code, rec.Body.String())
	}
	var body api.BenchmarkDetail
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应不是合法 JSON: %v", err)
	}
	if len(body.Versions) != 0 || len(body.Metrics) != 0 {
		t.Fatalf("空榜应当两个数组都为空：versions=%d metrics=%d", len(body.Versions), len(body.Metrics))
	}

	// 不存在的 slug：404。
	rec = httptest.NewRecorder()
	h.GetBenchmark(rec, httptest.NewRequest(http.MethodGet, "/api/v1/benchmarks/nope", nil), "nope")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("不存在的榜 = %d，想要 404", rec.Code)
	}
}
