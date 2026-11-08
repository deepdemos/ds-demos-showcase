package httpapi

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/identity"
)

// 作品榜（SOC-013 六口径 / SOC-014 零评分 / SOC-016 时间范围 / SOC-017 只含已上架）。
//
// 这一组用例的骨架：造几件统计数刻意不同的作品，然后逐个榜断言**顺序**。
// 只断言「条数」或「不为空」是抓不到口径错误的——六个榜在大多数数据上条数一样。

func newRankingHandler(t *testing.T) (*Handler, *identity.Service, *pgxpool.Pool) {
	t.Helper()
	return newSortHandler(t)
}

type ratingStats struct {
	avg   float64
	count int64
	god   int64
	ghost int64
	view  int64
	dl    int64
}

// publishRated 造一件公开作品，写死它的统计行，并可按需把它挪到过去。
func publishRated(t *testing.T, h *Handler, pool *pgxpool.Pool, sid, title string, s ratingStats, createdAgo time.Duration) string {
	t.Helper()
	slug := submitHTML(t, h, sid, title, `<!doctype html><p>rank</p>`)
	publishNow(t, pool, slug)
	if _, err := pool.Exec(context.Background(), `
INSERT INTO demo_stats (demo_id, view_count, download_count, rating_sum, rating_count, rating_avg, rating_god, rating_ghost)
SELECT id, $2::bigint, $3::bigint, round($4::numeric * $5::numeric)::bigint, $5::bigint, $4::numeric, $6::bigint, $7::bigint
FROM demos WHERE slug = $1
ON CONFLICT (demo_id) DO UPDATE
SET view_count = EXCLUDED.view_count,
    download_count = EXCLUDED.download_count,
    rating_sum = EXCLUDED.rating_sum,
    rating_count = EXCLUDED.rating_count,
    rating_avg = EXCLUDED.rating_avg,
    rating_god = EXCLUDED.rating_god,
    rating_ghost = EXCLUDED.rating_ghost`,
		slug, s.view, s.dl, s.avg, s.count, s.god, s.ghost); err != nil {
		t.Fatalf("写统计失败: %v", err)
	}
	if createdAgo > 0 {
		// 把创建时间推到过去：range 按创建时间过滤（SOC-016），所以这是唯一能造出
		// 「老作品」的方式——不能靠改 updated_at，那不影响 range 判据。
		if _, err := pool.Exec(context.Background(),
			`UPDATE demos SET created_at = now() - $2::interval, updated_at = now() - $2::interval WHERE slug = $1`,
			slug, createdAgo.String()); err != nil {
			t.Fatalf("改创建时间失败: %v", err)
		}
	}
	return slug
}

func rankSlugs(t *testing.T, h *Handler, query string) []string {
	t.Helper()
	rec := getWithSession(t, h, "/api/v1/rankings"+query, "")
	if rec.Code != 200 {
		t.Fatalf("榜单 %s = %d，想要 200；响应体：%s", query, rec.Code, rec.Body.String())
	}
	var body struct {
		Items []api.RankingEntry `json:"items"`
		Total int                `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("解析榜单失败: %v", err)
	}
	out := make([]string, 0, len(body.Items))
	for _, it := range body.Items {
		out = append(out, it.Demo.Slug)
	}
	return out
}

func assertOrder(t *testing.T, board string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s 榜条数 = %d（%v），想要 %d（%v）", board, len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s 榜顺序 = %v，想要 %v", board, got, want)
		}
	}
}

// 六个口径必须给出不同的顺序，且零评分作品只出现在 count / heat 榜（SOC-014）。
func TestIntegrationRankingsSixBoards(t *testing.T) {
	h, idSvc, pool := newRankingHandler(t)
	author, sid := registerVerified(t, pool, idSvc, registerInputFor(t, "榜单作者"))
	t.Cleanup(func() { cleanupAuthor(t, pool, author.Sub) })
	resetPublished(t, pool)

	// 甲：高分但票少；乙：分低但票多；丙：零票，浏览少；丁：零票，浏览多。
	a := publishRated(t, h, pool, sid, "榜单甲", ratingStats{avg: 4.8, count: 5, god: 4, view: 10}, 0)
	b := publishRated(t, h, pool, sid, "榜单乙", ratingStats{avg: 4.0, count: 20, god: 2, ghost: 1, view: 100}, 0)
	c := publishRated(t, h, pool, sid, "榜单丙", ratingStats{view: 5}, 0)
	d := publishRated(t, h, pool, sid, "榜单丁", ratingStats{view: 500}, 0)

	// avg：排除零票的丙丁，甲 4.8 > 乙 4.0。
	assertOrder(t, "avg", rankSlugs(t, h, "?board=avg"), []string{a, b})
	// god：甲 4 > 乙 2。
	assertOrder(t, "god", rankSlugs(t, h, "?board=god"), []string{a, b})
	// ghost：乙有 1 张 1 分票，甲 0 张——两者都有票，所以都上榜。
	assertOrder(t, "ghost", rankSlugs(t, h, "?board=ghost"), []string{b, a})
	// net：甲 4、乙 1。
	assertOrder(t, "net", rankSlugs(t, h, "?board=net"), []string{a, b})
	// count：乙 20 > 甲 5 > 丙/丁 0；丙丁同分（0 票）按较晚登记优先 → 丁在丙前。
	assertOrder(t, "count", rankSlugs(t, h, "?board=count"), []string{b, a, d, c})
	// heat：乙 100+0+20=120；丁 500；丙 5；甲 10+5=15 → 丁 乙 甲 丙。
	assertOrder(t, "heat", rankSlugs(t, h, "?board=heat"), []string{d, b, a, c})
}

// range 按作品创建时间过滤（SOC-016），不是按投票时间。
func TestIntegrationRankingsRangeUsesCreatedAt(t *testing.T) {
	h, idSvc, pool := newRankingHandler(t)
	author, sid := registerVerified(t, pool, idSvc, registerInputFor(t, "范围作者"))
	t.Cleanup(func() { cleanupAuthor(t, pool, author.Sub) })
	resetPublished(t, pool)

	old := publishRated(t, h, pool, sid, "四十天前", ratingStats{avg: 5, count: 10, god: 10}, 40*24*time.Hour)
	fresh := publishRated(t, h, pool, sid, "今天", ratingStats{avg: 3, count: 1, god: 1}, 0)

	assertOrder(t, "week", rankSlugs(t, h, "?board=avg&range=week"), []string{fresh})
	// 全部范围里两件都在，且老的分数更高，所以排前面。
	assertOrder(t, "all", rankSlugs(t, h, "?board=avg&range=all"), []string{old, fresh})
}

// 下架即从所有榜消失（SOC-017）。
func TestIntegrationRankingsExcludesUnlisted(t *testing.T) {
	h, idSvc, pool := newRankingHandler(t)
	author, sid := registerVerified(t, pool, idSvc, registerInputFor(t, "下架作者"))
	t.Cleanup(func() { cleanupAuthor(t, pool, author.Sub) })
	resetPublished(t, pool)

	keep := publishRated(t, h, pool, sid, "留着", ratingStats{avg: 4, count: 3, god: 2}, 0)
	gone := publishRated(t, h, pool, sid, "要被下架", ratingStats{avg: 5, count: 9, god: 9}, 0)

	assertOrder(t, "avg", rankSlugs(t, h, "?board=avg"), []string{gone, keep})
	if _, err := pool.Exec(context.Background(),
		`UPDATE demos SET lifecycle_state = 'archived' WHERE slug = $1`, gone); err != nil {
		t.Fatalf("下架失败: %v", err)
	}
	assertOrder(t, "avg", rankSlugs(t, h, "?board=avg"), []string{keep})
	assertOrder(t, "heat", rankSlugs(t, h, "?board=heat"), []string{keep})
}

// 并列规则（附录 D）：平均分榜同分时票多者在前；票也一样时较晚登记优先。
func TestIntegrationRankingsTieBreak(t *testing.T) {
	h, idSvc, pool := newRankingHandler(t)
	author, sid := registerVerified(t, pool, idSvc, registerInputFor(t, "并列作者"))
	t.Cleanup(func() { cleanupAuthor(t, pool, author.Sub) })
	resetPublished(t, pool)

	many := publishRated(t, h, pool, sid, "票多", ratingStats{avg: 4.5, count: 10, god: 5}, 0)
	few := publishRated(t, h, pool, sid, "票少", ratingStats{avg: 4.5, count: 2, god: 1}, 0)

	assertOrder(t, "avg", rankSlugs(t, h, "?board=avg"), []string{many, few})
}

// 分页：第二页的名次接着数（RankingEntry.rank 是连续名次，不是行号）。
func TestIntegrationRankingsRankContinuesAcrossPages(t *testing.T) {
	h, idSvc, pool := newRankingHandler(t)
	author, sid := registerVerified(t, pool, idSvc, registerInputFor(t, "翻页作者"))
	t.Cleanup(func() { cleanupAuthor(t, pool, author.Sub) })
	resetPublished(t, pool)

	publishRated(t, h, pool, sid, "第一名", ratingStats{avg: 5, count: 9, god: 9}, 0)
	publishRated(t, h, pool, sid, "第二名", ratingStats{avg: 4, count: 5, god: 3}, 0)

	rec := getWithSession(t, h, "/api/v1/rankings?board=avg&limit=1&offset=1", "")
	if rec.Code != 200 {
		t.Fatalf("第二页 = %d，想要 200；响应体：%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Items []api.RankingEntry `json:"items"`
		Total int                `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if body.Total != 2 {
		t.Fatalf("total = %d，想要 2", body.Total)
	}
	if len(body.Items) != 1 {
		t.Fatalf("第二页条数 = %d，想要 1", len(body.Items))
	}
	if body.Items[0].Rank != 2 {
		t.Fatalf("第二页第一条名次 = %d，想要 2", body.Items[0].Rank)
	}
}

// 参数非法回 400，不静默回落。
func TestIntegrationRankingsRejectsBadParams(t *testing.T) {
	h, idSvc, pool := newRankingHandler(t)
	author, sid := registerVerified(t, pool, idSvc, registerInputFor(t, "参数作者"))
	t.Cleanup(func() { cleanupAuthor(t, pool, author.Sub) })

	cases := []string{
		"/api/v1/rankings?board=bogus",
		"/api/v1/rankings?range=year",
		"/api/v1/rankings?limit=0",
		"/api/v1/rankings?limit=101",
		"/api/v1/rankings?offset=-1",
	}
	for _, path := range cases {
		rec := getWithSession(t, h, path, sid)
		if rec.Code != 400 {
			t.Fatalf("%s = %d，想要 400；响应体：%s", path, rec.Code, rec.Body.String())
		}
	}
}

// 空榜是 200 + 空数组，不是 404（界面的空态要用它自己的文案）。
func TestIntegrationRankingsEmptyIsNotAnError(t *testing.T) {
	h, idSvc, pool := newRankingHandler(t)
	author, sid := registerVerified(t, pool, idSvc, registerInputFor(t, "空榜作者"))
	t.Cleanup(func() { cleanupAuthor(t, pool, author.Sub) })
	resetPublished(t, pool)

	rec := getWithSession(t, h, "/api/v1/rankings?board=avg", sid)
	if rec.Code != 200 {
		t.Fatalf("空榜 = %d，想要 200；响应体：%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Items []api.RankingEntry `json:"items"`
		Total int                `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if body.Total != 0 || len(body.Items) != 0 {
		t.Fatalf("空榜 total=%d items=%d，想要 0/0", body.Total, len(body.Items))
	}
	if body.Items == nil {
		t.Fatal("items 应是空数组而不是 null：契约里是 required 字段，null 会让前端多一条分支")
	}
}
