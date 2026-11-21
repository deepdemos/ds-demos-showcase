package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
)

// 第三方基准快照导入（#526）。
//
// 这一组盯的是「dryRun 真的不写」「真实导入幂等」「闸门拦得住」三件事 ——
// 都是导入端点最贵的失败形态：写了一半、重导一次翻倍、把禁分发的源写进分数表。

const benchmarkImportDryRun = `{"dryRun":true,"payload":{"benchmarks":[{"slug":"it-bench-mark","name":"IT 基准","publisher":"IT","homepageUrl":"https://it.example/bench","category":"coding","displayTier":"table","license":"CC-BY","licenseUrl":"","attribution":"IT Corp"}],"versions":[{"benchmarkSlug":"it-bench-mark","version":"2026-11","asOf":"2026-11-01","sourceType":"leaderboard","coverage":"full","participantCount":12}],"metrics":[{"benchmarkSlug":"it-bench-mark","version":"2026-11","key":"score","label":"得分","isPrimary":true}],"scores":[{"benchmarkSlug":"it-bench-mark","version":"2026-11","metricKey":"score","externalName":"IT-Matched Model","rawScore":42.5,"rank":1},{"benchmarkSlug":"it-bench-mark","version":"2026-11","metricKey":"score","externalName":"Unknown Upstream Name","rawScore":30,"rank":2}]}}`

const benchmarkImportReal = `{"dryRun":false,"payload":{"benchmarks":[{"slug":"it-bench-mark","name":"IT 基准","publisher":"IT","homepageUrl":"https://it.example/bench","category":"coding","displayTier":"table","license":"CC-BY","licenseUrl":"","attribution":"IT Corp"}],"versions":[{"benchmarkSlug":"it-bench-mark","version":"2026-11","asOf":"2026-11-01","sourceType":"leaderboard","coverage":"full","participantCount":12}],"metrics":[{"benchmarkSlug":"it-bench-mark","version":"2026-11","key":"score","label":"得分","isPrimary":true}],"scores":[{"benchmarkSlug":"it-bench-mark","version":"2026-11","metricKey":"score","externalName":"IT-Matched Model","rawScore":42.5,"rank":1},{"benchmarkSlug":"it-bench-mark","version":"2026-11","metricKey":"score","externalName":"Unknown Upstream Name","rawScore":30,"rank":2}]}}`

// cite 档带分数：这类源禁止经 API 分发，整批要拒。
const benchmarkImportCiteTier = `{"dryRun":false,"payload":{"benchmarks":[{"slug":"it-bench-mark","name":"IT 基准","publisher":"IT","homepageUrl":"https://it.example/bench","category":"coding","displayTier":"cite","license":"CC-BY","licenseUrl":"","attribution":"IT Corp"}],"versions":[{"benchmarkSlug":"it-bench-mark","version":"2026-11","asOf":"2026-11-01","sourceType":"leaderboard","coverage":"full","participantCount":12}],"metrics":[{"benchmarkSlug":"it-bench-mark","version":"2026-11","key":"score","label":"得分","isPrimary":true}],"scores":[{"benchmarkSlug":"it-bench-mark","version":"2026-11","metricKey":"score","externalName":"IT-Matched Model","rawScore":42.5,"rank":1},{"benchmarkSlug":"it-bench-mark","version":"2026-11","metricKey":"score","externalName":"Unknown Upstream Name","rawScore":30,"rank":2}]}}`

func importBenchmarks(t *testing.T, h *Handler, sid, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ImportBenchmarks(rec, proposalPublicReq(http.MethodPost, "/api/v1/admin/benchmarks/import", sid, body))
	return rec
}

func decodeImportResult(t *testing.T, rec *httptest.ResponseRecorder) api.BenchmarkImportResult {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("导入 = %d，想要 200（body=%s）", rec.Code, rec.Body.String())
	}
	var out api.BenchmarkImportResult
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("响应不是合法 JSON: %v", err)
	}
	return out
}

func countBenchmarkRows(t *testing.T, h *Handler) int {
	t.Helper()
	var n int
	if err := h.pool.QueryRow(context.Background(), "SELECT count(*) FROM benchmarks WHERE slug = $1", "it-bench-mark").Scan(&n); err != nil {
		t.Fatalf("查基准失败: %v", err)
	}
	return n
}

func cleanupBenchmark(t *testing.T, h *Handler) {
	t.Helper()
	ctx := context.Background()
	_, _ = h.pool.Exec(ctx, `DELETE FROM benchmark_scores WHERE metric_id IN (
        SELECT bm.id FROM benchmark_metrics bm
          JOIN benchmark_versions bv ON bv.id = bm.version_id
          JOIN benchmarks b ON b.id = bv.benchmark_id WHERE b.slug = $1)`, "it-bench-mark")
	_, _ = h.pool.Exec(ctx, `DELETE FROM benchmark_metrics WHERE version_id IN (
        SELECT bv.id FROM benchmark_versions bv JOIN benchmarks b ON b.id = bv.benchmark_id WHERE b.slug = $1)`, "it-bench-mark")
	_, _ = h.pool.Exec(ctx, `DELETE FROM benchmark_versions WHERE benchmark_id IN (SELECT id FROM benchmarks WHERE slug = $1)`, "it-bench-mark")
	_, _ = h.pool.Exec(ctx, `DELETE FROM benchmarks WHERE slug = $1`, "it-bench-mark")
}

func TestIntegrationImportBenchmarksDryRunDoesNotWrite(t *testing.T) {
	h, idSvc, _ := newSortHandler(t)
	admin := mkAdmin(t, h.pool, idSvc, "benchimp")
	t.Cleanup(func() { cleanupBenchmark(t, h) })

	result := decodeImportResult(t, importBenchmarks(t, h, admin, benchmarkImportDryRun))
	if !result.DryRun {
		t.Fatalf("dryRun = false，想要 true")
	}
	if result.Created.Benchmarks != 1 || result.Created.Scores != 2 {
		t.Fatalf("新建条数 = %+v，想要 1 条榜 / 2 条分数", result.Created)
	}
	if got := countBenchmarkRows(t, h); got != 0 {
		t.Fatalf("dryRun 之后库里有 %d 行，想要 0", got)
	}
}

func TestIntegrationImportBenchmarksIsIdempotent(t *testing.T) {
	h, idSvc, _ := newSortHandler(t)
	admin := mkAdmin(t, h.pool, idSvc, "benchimp2")
	t.Cleanup(func() { cleanupBenchmark(t, h) })

	first := decodeImportResult(t, importBenchmarks(t, h, admin, benchmarkImportReal))
	if first.Created.Benchmarks != 1 || first.Created.Scores != 2 {
		t.Fatalf("第一次 = %+v，想要全新建", first.Created)
	}
	if got := countBenchmarkRows(t, h); got != 1 {
		t.Fatalf("导入后 = %d 行，想要 1", got)
	}

	second := decodeImportResult(t, importBenchmarks(t, h, admin, benchmarkImportDryRun))
	if second.Created.Benchmarks != 0 || second.Created.Versions != 0 || second.Created.Scores != 0 {
		t.Fatalf("第二次 dryRun 还报新建 = %+v，想要 0", second.Created)
	}
	if second.Updated.Benchmarks != 1 || second.Updated.Scores != 2 {
		t.Fatalf("第二次 dryRun 更新 = %+v，想要 1 / 2", second.Updated)
	}
}

func TestIntegrationImportBenchmarksRejectsCiteTierScores(t *testing.T) {
	h, idSvc, _ := newSortHandler(t)
	admin := mkAdmin(t, h.pool, idSvc, "benchimp3")
	t.Cleanup(func() { cleanupBenchmark(t, h) })

	rec := importBenchmarks(t, h, admin, benchmarkImportCiteTier)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("cite 档带分数 = %d，想要 422：%s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, "禁止经 API 分发") {
		t.Fatalf("422 文案没讲清理由：%s", body)
	}
	if got := countBenchmarkRows(t, h); got != 0 {
		t.Fatalf("被拒之后库里有 %d 行，想要 0", got)
	}
}

func TestIntegrationImportBenchmarksUnmatchedModel(t *testing.T) {
	h, idSvc, _ := newSortHandler(t)
	admin := mkAdmin(t, h.pool, idSvc, "benchimp4")
	t.Cleanup(func() { cleanupBenchmark(t, h) })

	result := decodeImportResult(t, importBenchmarks(t, h, admin, benchmarkImportReal))
	if len(result.UnmatchedModels) != 2 {
		t.Fatalf("未匹配清单 = %v，想要两条（库里没有这些模型）", result.UnmatchedModels)
	}
	var status string
	if err := h.pool.QueryRow(context.Background(), `
SELECT bs.match_status FROM benchmark_scores bs
  JOIN benchmark_metrics bm ON bm.id = bs.metric_id
  JOIN benchmark_versions bv ON bv.id = bm.version_id
  JOIN benchmarks b ON b.id = bv.benchmark_id
 WHERE b.slug = $1 AND bs.rank = 1`, "it-bench-mark").Scan(&status); err != nil {
		t.Fatalf("查分数失败: %v", err)
	}
	if status != "unmatched" {
		t.Fatalf("match_status = %q，想要 unmatched", status)
	}
}

func TestIntegrationImportBenchmarksRequiresPermission(t *testing.T) {
	h, idSvc, _ := newSortHandler(t)
	author, sid := registerVerified(t, h.pool, idSvc, registerInputFor(t, "benchimp5"))
	t.Cleanup(func() { cleanupAuthor(t, h.pool, author.Sub) })

	if rec := importBenchmarks(t, h, sid, benchmarkImportDryRun); rec.Code != http.StatusForbidden {
		t.Fatalf("没有 benchmark.import = %d，想要 403：%s", rec.Code, rec.Body.String())
	}
}
