package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/identity"
	"github.com/jackc/pgx/v5/pgxpool"
)

// 模型页的读路径（模型域 §5.1，scope C 三块全做）。
//
// 这一组用例压的是三件事：
//   1. 站内表现只算**公开可见**作品，且计数与列表同源；
//   2. 档案没有时是 null（不是空对象），有档案时缺值是 null（不是 0）；
//   3. 基准只列**参与综合分资格**的条目——不满足的留在库里但不进页面。

// modelProfile 打一次模型页并解析。
func modelProfile(t *testing.T, h *Handler, slug string) (*httptest.ResponseRecorder, api.ModelProfile) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/models/"+slug, nil)
	rec := httptest.NewRecorder()
	h.GetModel(rec, req, slug)
	var body api.ModelProfile
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("解析模型页失败: %v；响应体：%s", err, rec.Body.String())
		}
	}
	return rec, body
}

// publishDemoWithModel 造一件声明了这个模型的作品（是否公开由调用方决定）。
func publishDemoWithModel(t *testing.T, h *Handler, sid, title, modelSlug string) string {
	t.Helper()
	rec := submitHTMLWithModels(t, h, sid, title, `<!doctype html><p>hi</p>`, []string{modelSlug})
	if rec.Code != http.StatusCreated {
		t.Fatalf("投稿 = %d，想要 201；响应体：%s", rec.Code, rec.Body.String())
	}
	var created struct {
		Slug string `json:"slug"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil || created.Slug == "" {
		t.Fatalf("解析投稿回执失败: %v；响应体：%s", err, rec.Body.String())
	}
	return created.Slug
}

// newModelProfileHandler 与 newModelTestHandler 同形，但档案用例要直接写 model_specs，
// 于是单独给它一个名字：读起来能看出「这条用例动过档案表」。
func newModelProfileHandler(t *testing.T) (*Handler, *identity.Service, *pgxpool.Pool) {
	t.Helper()
	return newModelTestHandler(t)
}

// registerInputFor 造一个可注册的输入（测试里只需要邮箱/用户名唯一）。
func registerInputFor(t *testing.T, display string) identity.RegisterInput {
	t.Helper()
	tag := uniq()
	return identity.RegisterInput{
		Email:       "it-" + tag + "@example.test",
		Password:    "it-password-12345",
		Username:    "it" + tag,
		DisplayName: display,
	}
}

// 公开作品进统计、进列表；pending 与 archived 都不进。
func TestIntegrationModelPageCountsPublicOnly(t *testing.T) {
	h, idSvc, pool := newModelTestHandler(t)
	slug := "it-page-" + uniq()
	insertModel(t, pool, slug, "模型页用例", "active")

	acc, sid := registerVerified(t, pool, idSvc, registerInputFor(t, "模型页作者"))
	t.Cleanup(func() { cleanupAuthor(t, pool, acc.Sub) })

	pub := publishDemoWithModel(t, h, sid, "公开的那件", slug)
	publishNow(t, pool, pub)

	pending := publishDemoWithModel(t, h, sid, "还在审的那件", slug)

	archived := publishDemoWithModel(t, h, sid, "已下架的那件", slug)
	publishNow(t, pool, archived)
	if _, err := pool.Exec(context.Background(),
		`UPDATE demos SET lifecycle_state = 'archived' WHERE slug = $1`, archived); err != nil {
		t.Fatalf("造下架态失败: %v", err)
	}

	rec, body := modelProfile(t, h, slug)
	if rec.Code != http.StatusOK {
		t.Fatalf("模型页 = %d，想要 200；响应体：%s", rec.Code, rec.Body.String())
	}
	if body.Site.DemoCount != 1 {
		t.Fatalf("公开作品数 = %d，想要 1（pending 与 archived 都不算）", body.Site.DemoCount)
	}
	if len(body.Demos) != 1 || body.Demos[0].Slug != pub {
		t.Fatalf("作品列表 = %+v，想要只有 %s", body.Demos, pub)
	}
	_ = pending

	// 计数与列表必须同源：模型页说 1 件，作品库按 model 筛也该是 1 条。
	list := getWithSession(t, h, "/api/v1/demos?model="+slug, "")
	if list.Code != http.StatusOK {
		t.Fatalf("作品库筛选 = %d，想要 200", list.Code)
	}
	var listing struct {
		Total int `json:"total"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &listing); err != nil {
		t.Fatalf("解析列表失败: %v", err)
	}
	if listing.Total != body.Site.DemoCount {
		t.Fatalf("作品库筛出 %d 条，模型页说 %d 件——两处判据分裂了",
			listing.Total, body.Site.DemoCount)
	}
}

// 没有档案时 specs 是 null，不是空对象。
func TestIntegrationModelPageSpecsNullWhenMissing(t *testing.T) {
	h, _, pool := newModelTestHandler(t)
	slug := "it-nospec-" + uniq()
	insertModel(t, pool, slug, "没有档案的模型", "active")

	rec, body := modelProfile(t, h, slug)
	if rec.Code != http.StatusOK {
		t.Fatalf("模型页 = %d，想要 200；响应体：%s", rec.Code, rec.Body.String())
	}
	if body.Specs != nil {
		t.Fatalf("没有档案时 specs 应为 null，实际 %+v", body.Specs)
	}
}

// 有档案时逐字段映射；没给的字段是 null 而不是 0。
func TestIntegrationModelPageSpecsMapping(t *testing.T) {
	h, _, pool := newModelProfileHandler(t)
	slug := "it-spec-" + uniq()
	insertModel(t, pool, slug, "有档案的模型", "active")

	var modelID int64
	if err := pool.QueryRow(context.Background(),
		`SELECT id FROM models WHERE slug = $1`, slug).Scan(&modelID); err != nil {
		t.Fatalf("取模型 id 失败: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO model_specs (model_id, context_window, max_output, release_date, license, source)
		VALUES ($1, 1048576, NULL, DATE '2026-03-01', 'MIT', 'manual')`, modelID); err != nil {
		t.Fatalf("写档案失败: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM model_specs WHERE model_id = $1`, modelID)
	})

	_, body := modelProfile(t, h, slug)
	if body.Specs == nil {
		t.Fatal("有档案时 specs 不该是 null")
	}
	if body.Specs.ContextWindow == nil || *body.Specs.ContextWindow != 1048576 {
		t.Fatalf("contextWindow 映射错了：%+v", body.Specs.ContextWindow)
	}
	if body.Specs.MaxOutput != nil {
		t.Fatalf("没给的 maxOutput 应为 null，实际 %v", *body.Specs.MaxOutput)
	}
	if body.Specs.ReleaseDate == nil {
		t.Fatal("releaseDate 该有值")
	}
}

// 票数为 0 时 scored=false：界面据此不显示「0 分」。
func TestIntegrationModelPageNotScoredWithoutVotes(t *testing.T) {
	h, idSvc, pool := newModelTestHandler(t)
	slug := "it-novote-" + uniq()
	insertModel(t, pool, slug, "还没人评分的模型", "active")

	acc, sid := registerVerified(t, pool, idSvc, registerInputFor(t, "无票作者"))
	t.Cleanup(func() { cleanupAuthor(t, pool, acc.Sub) })
	demo := publishDemoWithModel(t, h, sid, "没票的作品", slug)
	publishNow(t, pool, demo)

	_, body := modelProfile(t, h, slug)
	if body.Site.Scored {
		t.Fatalf("没人投票时 scored 应为 false，实际 true（score=%v）", body.Site.Score)
	}
	if body.Site.Votes != 0 {
		t.Fatalf("票数 = %d，想要 0", body.Site.Votes)
	}
}

// 有票之后 scored=true，且分数落在 1..5 之间（贝叶斯收缩不会跑到区间外）。
func TestIntegrationModelPageScoredWithVotes(t *testing.T) {
	h, idSvc, pool := newModelTestHandler(t)
	slug := "it-vote-" + uniq()
	insertModel(t, pool, slug, "有票的模型", "active")

	acc, sid := registerVerified(t, pool, idSvc, registerInputFor(t, "投票作者"))
	t.Cleanup(func() { cleanupAuthor(t, pool, acc.Sub) })
	demo := publishDemoWithModel(t, h, sid, "有票的作品", slug)
	publishNow(t, pool, demo)

	if rec := putRating(t, h, demo, sid, 5); rec.Code != http.StatusOK {
		t.Fatalf("打分 = %d，想要 200；响应体：%s", rec.Code, rec.Body.String())
	}

	_, body := modelProfile(t, h, slug)
	if !body.Site.Scored {
		t.Fatal("有 1 票之后 scored 应为 true")
	}
	if body.Site.Votes != 1 {
		t.Fatalf("票数 = %d，想要 1", body.Site.Votes)
	}
	if body.Site.Score < 1 || body.Site.Score > 5 {
		t.Fatalf("社区分 = %v，应当落在 1..5", body.Site.Score)
	}
}

// 未知 slug 是 404，不是空页面。
func TestIntegrationModelPageNotFound(t *testing.T) {
	h, _, _ := newModelTestHandler(t)
	rec, _ := modelProfile(t, h, "it-missing-"+uniq())
	if rec.Code != http.StatusNotFound {
		t.Fatalf("未知模型的模型页 = %d，想要 404；响应体：%s", rec.Code, rec.Body.String())
	}
}

// 基准只列参与综合分资格的条目：参赛数 < 3、自报、未排名都不进页面。
func TestIntegrationModelPageBenchmarksFiltered(t *testing.T) {
	h, _, pool := newModelProfileHandler(t)
	slug := "it-bench-" + uniq()
	insertModel(t, pool, slug, "有基准的模型", "active")

	var modelID int64
	if err := pool.QueryRow(context.Background(),
		`SELECT id FROM models WHERE slug = $1`, slug).Scan(&modelID); err != nil {
		t.Fatalf("取模型 id 失败: %v", err)
	}

	tag := uniq()
	valid := "it-valid-" + tag
	small := "it-small-" + tag
	self := "it-self-" + tag

	ctx := context.Background()
	insertBench := func(benchSlug, version string, participants int, sourceType string) int64 {
		var benchID int64
		if err := pool.QueryRow(ctx,
			`INSERT INTO benchmarks (slug, name, status) VALUES ($1, $1, 'active') RETURNING id`,
			benchSlug).Scan(&benchID); err != nil {
			t.Fatalf("建榜失败: %v", err)
		}
		var versionID int64
		if err := pool.QueryRow(ctx, `
			INSERT INTO benchmark_versions (benchmark_id, version, as_of, source_type, coverage, participant_count, status)
			VALUES ($1, $2, DATE '2026-05-01', $3, 'full', $4, 'active') RETURNING id`,
			benchID, version, sourceType, participants).Scan(&versionID); err != nil {
			t.Fatalf("建版本失败: %v", err)
		}
		var metricID int64
		if err := pool.QueryRow(ctx, `
			INSERT INTO benchmark_metrics (version_id, key, label, is_primary)
			VALUES ($1, 'score', '综合', true) RETURNING id`, versionID).Scan(&metricID); err != nil {
			t.Fatalf("建指标失败: %v", err)
		}
		return metricID
	}

	metricValid := insertBench(valid, "v1", 20, "leaderboard")
	metricSmall := insertBench(small, "v1", 2, "leaderboard")
	metricSelf := insertBench(self, "v1", 20, "self_reported")

	insertScore := func(metricID int64, rank int) {
		if _, err := pool.Exec(ctx, `
			INSERT INTO benchmark_scores (metric_id, model_id, external_name, external_name_key,
			                              match_status, raw_score, rank, ranked)
			VALUES ($1, $2, $3, $3, 'matched', 88.5, $4, true)`,
			metricID, modelID, slug+"-"+tag, rank); err != nil {
			t.Fatalf("写分数失败: %v", err)
		}
	}
	insertScore(metricValid, 3)
	insertScore(metricSmall, 1)
	insertScore(metricSelf, 2)

	t.Cleanup(func() {
		for _, bs := range []string{valid, small, self} {
			_, _ = pool.Exec(ctx, `DELETE FROM benchmarks WHERE slug = $1`, bs)
		}
	})

	_, body := modelProfile(t, h, slug)
	if len(body.Benchmarks) != 1 {
		t.Fatalf("基准条目 = %d，想要 1（参赛数 < 3 与自报都该被排除）：%+v",
			len(body.Benchmarks), body.Benchmarks)
	}
	if body.Benchmarks[0].BenchmarkSlug != valid {
		t.Fatalf("留下的那条 = %s，想要 %s", body.Benchmarks[0].BenchmarkSlug, valid)
	}
	if body.Benchmarks[0].Rank == nil || *body.Benchmarks[0].Rank != 3 {
		t.Fatalf("名次 = %v，想要 3", body.Benchmarks[0].Rank)
	}
}
