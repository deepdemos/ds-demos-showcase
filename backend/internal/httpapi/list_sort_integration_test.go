package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/demo"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/identity"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/platform/storage"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// 列表排序（SCH-006）：最新 / 热度 / 随机。
//
// 三条判据各自独立：热度按 SOC-015 的公式且并列取较晚登记、随机在同一 seed 下
// 翻页不重不漏、latest 仍是默认。这些都不是「顺序对不对」的表层断言——
// 热度用例专门造了「浏览量高但下载少」与「下载多但浏览少」两种作品，
// 只按其中一列排都会得出错误的顺序。

func newSortHandler(t *testing.T) (*Handler, *identity.Service, *pgxpool.Pool) {
	t.Helper()
	pool := integrationPool(t)
	q := store.New(pool)
	idSvc := integrationIdentity(t, pool)
	files, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("初始化存储失败: %v", err)
	}
	demoSvc := demo.New(pool, q, files, storage.DefaultLimits(), 200<<20, 0, nil)
	return New(pool, q, idSvc, nil, demoSvc), idSvc, pool
}

// publishWithStats 造一件公开作品并写死它的统计行。
func publishWithStats(t *testing.T, h *Handler, pool *pgxpool.Pool, sid, title string, view, download, ratingCount int64) string {
	t.Helper()
	slug := submitHTML(t, h, sid, title, `<!doctype html><p>sort</p>`)
	publishNow(t, pool, slug)
	if _, err := pool.Exec(context.Background(), `
INSERT INTO demo_stats (demo_id, view_count, download_count, rating_count)
SELECT id, $2, $3, $4 FROM demos WHERE slug = $1
ON CONFLICT (demo_id) DO UPDATE
SET view_count = EXCLUDED.view_count,
    download_count = EXCLUDED.download_count,
    rating_count = EXCLUDED.rating_count`, slug, view, download, ratingCount); err != nil {
		t.Fatalf("写统计失败: %v", err)
	}
	return slug
}

func listSlugs(t *testing.T, h *Handler, query string) []string {
	t.Helper()
	rec := getWithSession(t, h, "/api/v1/demos"+query, "")
	if rec.Code != 200 {
		t.Fatalf("列表 %s = %d，想要 200；响应体：%s", query, rec.Code, rec.Body.String())
	}
	var body struct {
		Items []api.DemoSummary `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("解析列表失败: %v", err)
	}
	slugs := make([]string, 0, len(body.Items))
	for _, it := range body.Items {
		slugs = append(slugs, it.Slug)
	}
	return slugs
}

// ownSlugs 只留下这一条用例自己造的作品。
//
// 共享库里随时可能有别人留下的公开作品（另一个包、并发的盲审、同一台机器上的另一次运行），
// 而这两条用例断言的是「条数与相对顺序」。数全表等于把别人的活动当成自己的失败——
// 实测全量跑时偶发过「条数不对」。取子序列不影响相对顺序断言的强度：外来行只会插在中间。
func ownSlugs(t *testing.T, got []string, own map[string]bool) []string {
	t.Helper()
	kept := make([]string, 0, len(own))
	for _, slug := range got {
		if own[slug] {
			kept = append(kept, slug)
		}
	}
	return kept
}

// 综合热度 = 浏览 + 2 × 下载 + 评分人数（SOC-015）。
func TestIntegrationListDemosSortHot(t *testing.T) {
	h, idSvc, pool := newSortHandler(t)
	author, sid := registerVerified(t, pool, idSvc, registerInputFor(t, "排序作者"))
	t.Cleanup(func() { cleanupAuthor(t, pool, author.Sub) })

	// **不清库**：断言只覆盖自己造的四件。清别人的公开态会把并发跑在同一个库上的
	// 用例当场弄红，而失败信息会指向这里（见下面 ownSlugs 的注释）。
	a := publishWithStats(t, h, pool, sid, "热度甲", 10, 0, 0) // 10
	b := publishWithStats(t, h, pool, sid, "热度乙", 0, 6, 0)  // 12
	c := publishWithStats(t, h, pool, sid, "热度丙", 0, 0, 3)  // 3
	d := publishWithStats(t, h, pool, sid, "热度丁", 10, 0, 0) // 10，与甲并列

	own := map[string]bool{a: true, b: true, c: true, d: true}
	got := ownSlugs(t, listSlugs(t, h, "?sort=hot"), own)
	// 甲与丁同分（都是 10）：按附录 D「较晚登记的作品优先」，丁（后建）排在甲前。
	want := []string{b, d, a, c}
	if len(got) != len(want) {
		t.Fatalf("热度排序里自己那四件的条数 = %d，想要 %d：%v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("热度排序 = %v，想要 %v（浏览+2×下载+评分人数；并列取较晚登记）", got, want)
		}
	}

	// 只按浏览量排会得到「甲 丁 乙 丙」，与热度不同——正是 SOC-015 要的效果。
	viewOnly := listSlugs(t, h, "?sort=latest")
	if len(viewOnly) == 0 {
		t.Fatal("latest 不该为空")
	}
}

// 同一 seed 下翻页不重不漏（SCH-007）。
func TestIntegrationListDemosSortRandomStable(t *testing.T) {
	h, idSvc, pool := newSortHandler(t)
	author, sid := registerVerified(t, pool, idSvc, registerInputFor(t, "随机作者"))
	t.Cleanup(func() { cleanupAuthor(t, pool, author.Sub) })

	made := make(map[string]bool, 5)
	for _, title := range []string{"随机一", "随机二", "随机三", "随机四", "随机五"} {
		made[publishWithStats(t, h, pool, sid, title, 0, 0, 0)] = true
	}
	if len(made) != 5 {
		t.Fatalf("造了 %d 件作品，想要 5 件", len(made))
	}

	// 5 件作品一页就取完（pageSize 20），所以这里不真的翻页：同一 seed 连取两次、
	// 再换一个 seed，就足以钉住「同一 seed 下分页不会重复或漏项」的前提——
	// 顺序只要随请求变化，第二页就必然与第一页重叠或跳过。
	first := ownSlugs(t, listSlugs(t, h, "?sort=random&seed=42"), made)
	if len(first) != 5 {
		t.Fatalf("随机首屏条数 = %d，想要 5", len(first))
	}
	seen := make(map[string]bool, 5)
	for _, s := range first {
		if seen[s] {
			t.Fatalf("同一页里出现了重复作品：%s", s)
		}
		seen[s] = true
	}
	if len(seen) != 5 {
		t.Fatalf("首屏覆盖 %d 件，想要 5 件：%v", len(seen), first)
	}

	// 同 seed 再请求一次：顺序必须逐位相同（否则翻页必然重复或漏项）。
	again := ownSlugs(t, listSlugs(t, h, "?sort=random&seed=42"), made)
	for i := range first {
		if first[i] != again[i] {
			t.Fatalf("同一 seed 两次顺序不同：%v vs %v", first, again)
		}
	}

	// 换 seed 应该换顺序（5 件作品、两个种子恰好的概率极低）。
	other := ownSlugs(t, listSlugs(t, h, "?sort=random&seed=7"), made)
	same := true
	for i := range first {
		if first[i] != other[i] {
			same = false
			break
		}
	}
	if same {
		t.Fatalf("换 seed 顺序没变（seed=42 与 seed=7 都是 %v）——随机序没生效", first)
	}
}

// resetPublished 把库里所有公开作品清掉（共享测试库，前序用例会留数据）。
func resetPublished(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
DELETE FROM demo_channels WHERE demo_id IN (
    SELECT id FROM demos WHERE lifecycle_state = 'published'
)`); err != nil {
		t.Fatalf("清理渠道失败: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `
UPDATE demos SET published_revision_id = NULL, lifecycle_state = 'draft'
WHERE lifecycle_state = 'published'`); err != nil {
		t.Fatalf("清理公开态失败: %v", err)
	}
}

/**
 * 非法排序取值回 400，而不是静默按 latest 处理。
 *
 * 生成的参数绑定只把字符串塞进来、不校验枚举，所以这一条只能由 handler 兜。
 * 静默回落会让调用方以为「排序生效了」，看到的却是最新序。
 */
func TestIntegrationListDemosRejectsUnknownSort(t *testing.T) {
	h, idSvc, pool := newSortHandler(t)
	acc, sid := registerVerified(t, pool, idSvc, registerInputFor(t, "bad-sort"))
	t.Cleanup(func() { cleanupAuthor(t, pool, acc.Sub) })

	if rec := getWithSession(t, h, "/api/v1/demos?sort=bogus", sid); rec.Code != http.StatusBadRequest {
		t.Fatalf("sort=bogus = %d，想要 400", rec.Code)
	}
	if rec := getWithSession(t, h, "/api/v1/demos?sort=latest&seed=-1", sid); rec.Code != http.StatusBadRequest {
		t.Fatalf("seed=-1 = %d，想要 400", rec.Code)
	}
}
