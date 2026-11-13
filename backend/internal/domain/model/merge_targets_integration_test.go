package model

// 0030 把合并链的终点收进 model_merge_targets 视图，这里是四条读路径的库层用例。
//
// 为什么要有：ResolveModelRefs（#446）已经沿 merged_into_id 走到尽头，但 ListModels /
// ListRevisionModels / GetModelByID / GetModelBySlug 一直是单跳。A→B→C 时，同一个旧名字
// 在解析里得到 C、在展示里得到 B，而 B 自己也被合并掉了——两处指向不同模型，读者点到的
// 是两个语义互相矛盾的链接。库里现在 0 行合并数据，这条不一致只能靠用例先把形状钉住。
//
// 四条用例各钉一件事：两跳链走到链尾、环（2 环、3 环、拐进环的尾巴）与悬空指针都滤成
// 「没有归宿」且每行只出现一次、超过 20 层时停在第 20 层（与 ResolveModelRefs 同一套兜底）、
// 21 个成员的环因为超过 20 层认不出来也按上限停住（已知代价，迁移注释里写明）。

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// mergeTestModel 建一个模型。建与清理都复用 checkpoint 那套 helper，避免同一份收尾
// 逻辑写两遍——模型、调用行、版本的删除顺序在那边已经踩过一次。
func mergeTestModel(t *testing.T, pool *pgxpool.Pool, slug string) int64 {
	t.Helper()
	return mkCheckpointTestModel(t, pool, slug, "IT "+slug)
}

// mergeModels 把 from 标成已合并，并把归宿指向 to。
func mergeModels(t *testing.T, pool *pgxpool.Pool, from, to int64) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		"UPDATE models SET status = $2, merged_into_id = $3 WHERE id = $1",
		from, "deprecated", to); err != nil {
		t.Fatalf("合并模型 %d → %d 失败: %v", from, to, err)
	}
}

// mergedSlug 从 ListModels 的结果里取某个 slug 的合并归宿。
func mergedSlug(rows []store.ListModelsRow, slug string) (string, bool) {
	for _, r := range rows {
		if r.Slug == slug {
			return r.MergedIntoSlug, true
		}
	}
	return "", false
}

// mergeTestRevision 造一件作品与一个版本，让这个版本声明 modelID，返回 revision id。
// 收尾按依赖顺序删：先删声明、再删版本、最后删作品。
func mergeTestRevision(t *testing.T, pool *pgxpool.Pool, modelID int64) int64 {
	t.Helper()
	ctx := context.Background()
	slug := fmt.Sprintf("it-merge-demo-%d", time.Now().UnixNano())
	var demoID, revID int64
	if err := pool.QueryRow(ctx,
		"INSERT INTO demos (slug, title, moderation_state, lifecycle_state) VALUES ($1, $2, $3, $4) RETURNING id",
		slug, "IT merge demo", "approved", "published").Scan(&demoID); err != nil {
		t.Fatalf("插入作品失败: %v", err)
	}
	if err := pool.QueryRow(ctx,
		"INSERT INTO demo_revisions (demo_id, rev_no, title, demo_type) VALUES ($1, 1, $2, $3) RETURNING id",
		demoID, "IT merge demo", "web").Scan(&revID); err != nil {
		t.Fatalf("插入版本失败: %v", err)
	}
	if _, err := pool.Exec(ctx,
		"INSERT INTO demo_revision_models (revision_id, model_id) VALUES ($1, $2)", revID, modelID); err != nil {
		t.Fatalf("插入模型声明失败: %v", err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), "DELETE FROM demo_revision_models WHERE revision_id = $1", revID); err != nil {
			t.Logf("清理模型声明失败: %v", err)
		}
		if _, err := pool.Exec(context.Background(), "DELETE FROM demo_revisions WHERE id = $1", revID); err != nil {
			t.Logf("清理版本失败: %v", err)
		}
		if _, err := pool.Exec(context.Background(), "DELETE FROM demos WHERE id = $1", demoID); err != nil {
			t.Logf("清理作品失败: %v", err)
		}
	})
	return revID
}

func TestIntegrationReadPathsFollowMergeChainToEnd(t *testing.T) {
	pool := checkpointTestPool(t)
	ctx := context.Background()
	q := store.New(pool)

	slugs := []string{"it-merge-a", "it-merge-b", "it-merge-c"}
	t.Cleanup(func() { cleanupCheckpointTest(t, pool, slugs) })
	aID := mergeTestModel(t, pool, slugs[0])
	bID := mergeTestModel(t, pool, slugs[1])
	cID := mergeTestModel(t, pool, slugs[2])

	// A→B→C：A、B 都已合并，C 是归宿。
	mergeModels(t, pool, aID, bID)
	mergeModels(t, pool, bID, cID)

	listRows, err := q.ListModels(ctx, pgtype.Text{})
	if err != nil {
		t.Fatalf("ListModels 失败: %v", err)
	}
	for _, tc := range []struct{ slug, want string }{
		{"it-merge-a", "it-merge-c"},
		{"it-merge-b", "it-merge-c"},
		{"it-merge-c", ""},
	} {
		got, ok := mergedSlug(listRows, tc.slug)
		if !ok {
			t.Fatalf("ListModels 里没有 %s", tc.slug)
		}
		if got != tc.want {
			t.Errorf("ListModels：%s 的归宿是 %q，想要 %q", tc.slug, got, tc.want)
		}
	}

	// 视图只能给每个模型一行：JOIN 写错（比如再接一次 models）会让目录里出现重复行。
	count := map[string]int{}
	for _, r := range listRows {
		count[r.Slug]++
	}
	for _, s := range slugs {
		if count[s] != 1 {
			t.Errorf("ListModels 里 %s 出现了 %d 次，应当恰好一次", s, count[s])
		}
	}

	byID, err := q.GetModelByID(ctx, aID)
	if err != nil {
		t.Fatalf("GetModelByID 失败: %v", err)
	}
	if byID.MergedIntoSlug != "it-merge-c" {
		t.Errorf("GetModelByID：%s 的归宿是 %q，想要 it-merge-c", byID.Slug, byID.MergedIntoSlug)
	}

	bySlug, err := q.GetModelBySlug(ctx, "it-merge-a")
	if err != nil {
		t.Fatalf("GetModelBySlug 失败: %v", err)
	}
	if bySlug.MergedIntoSlug != "it-merge-c" {
		t.Errorf("GetModelBySlug：%s 的归宿是 %q，想要 it-merge-c", bySlug.Slug, bySlug.MergedIntoSlug)
	}

	// 作品页那条：作者用旧名字 A 声明，页面该显示 A 已经并到 C。
	revID := mergeTestRevision(t, pool, aID)
	revRows, err := q.ListRevisionModels(ctx, revID)
	if err != nil {
		t.Fatalf("ListRevisionModels 失败: %v", err)
	}
	if len(revRows) != 1 {
		t.Fatalf("ListRevisionModels 返回 %d 行，想要 1 行", len(revRows))
	}
	if revRows[0].MergedIntoSlug != "it-merge-c" {
		t.Errorf("ListRevisionModels：%s 的归宿是 %q，想要 it-merge-c", revRows[0].Slug, revRows[0].MergedIntoSlug)
	}
}

func TestIntegrationReadPathsTolerateCycleAndDangling(t *testing.T) {
	pool := checkpointTestPool(t)
	ctx := context.Background()
	q := store.New(pool)

	// x↔y 两环；p→q→r→p 三环；t 挂在三环的入口上（尾巴拐进环）；z 的归宿行被硬删。
	// 三环必须单独造：只有环长整除 20 时深度上限才会绕回起点，2 环（20 mod 2 = 0）
	// 恰好把「环给环内链接」这个窟窿盖住。
	slugs := []string{
		"it-merge-x", "it-merge-y",
		"it-merge-p", "it-merge-q", "it-merge-r",
		"it-merge-t", "it-merge-z",
		"it-merge-u", "it-merge-v",
	}
	t.Cleanup(func() { cleanupCheckpointTest(t, pool, slugs) })
	ids := make(map[string]int64, len(slugs))
	for _, slug := range slugs {
		ids[slug] = mergeTestModel(t, pool, slug)
	}

	// 两环：x→y→x。
	mergeModels(t, pool, ids["it-merge-x"], ids["it-merge-y"])
	mergeModels(t, pool, ids["it-merge-y"], ids["it-merge-x"])
	// 三环：p→q→r→p。
	mergeModels(t, pool, ids["it-merge-p"], ids["it-merge-q"])
	mergeModels(t, pool, ids["it-merge-q"], ids["it-merge-r"])
	mergeModels(t, pool, ids["it-merge-r"], ids["it-merge-p"])
	// 尾巴拐进三环：t→p，t 自己不在环上，但也走不到尽头。
	mergeModels(t, pool, ids["it-merge-t"], ids["it-merge-p"])
	// 悬空：归宿行被硬删之后指针还留着（models.merged_into_id 没有外键）。
	if _, err := pool.Exec(ctx,
		"UPDATE models SET status = $2, merged_into_id = $3 WHERE id = $1",
		ids["it-merge-z"], "deprecated", int64(900000000)); err != nil {
		t.Fatalf("制造悬空指针失败: %v", err)
	}
	// 链尾悬空：u→v，v 的归宿行被硬删。u 的终点仍是存在的 v（页面显示「并到 v」），
	// 只有 v 自己的终点是 v、被读路径滤成空——整条链不会一起消失。
	mergeModels(t, pool, ids["it-merge-u"], ids["it-merge-v"])
	if _, err := pool.Exec(ctx,
		"UPDATE models SET status = $2, merged_into_id = $3 WHERE id = $1",
		ids["it-merge-v"], "deprecated", int64(900000001)); err != nil {
		t.Fatalf("制造链尾悬空失败: %v", err)
	}

	rows, err := q.ListModels(ctx, pgtype.Text{})
	if err != nil {
		t.Fatalf("环与悬空指针下 ListModels 报错: %v", err)
	}
	// 环与「自己就是悬空节点」的模型：读路径一律看不到归宿，不会给出一个环形链接。
	// u/v 是链尾悬空，终点是 v（真实存在），另行断言。
	noDestination := slugs[:7]
	for _, s := range noDestination {
		got, ok := mergedSlug(rows, s)
		if !ok {
			t.Fatalf("ListModels 里没有 %s", s)
		}
		if got != "" {
			t.Errorf("ListModels：%s 在环或悬空指针下给出归宿 %q，应当是空", s, got)
		}
	}
	for _, s := range noDestination {
		row, err := q.GetModelBySlug(ctx, s)
		if err != nil {
			t.Fatalf("环或悬空指针下 GetModelBySlug(%s) 报错: %v", s, err)
		}
		if row.MergedIntoSlug != "" {
			t.Errorf("GetModelBySlug(%s)：环或悬空指针下给出归宿 %q，应当是空", s, row.MergedIntoSlug)
		}
	}
	for _, s := range []string{"it-merge-x", "it-merge-p"} {
		byID, err := q.GetModelByID(ctx, ids[s])
		if err != nil {
			t.Fatalf("环或悬空指针下 GetModelByID(%s) 报错: %v", s, err)
		}
		if byID.MergedIntoSlug != "" {
			t.Errorf("GetModelByID(%s)：环或悬空指针下给出归宿 %q，应当是空", s, byID.MergedIntoSlug)
		}
	}

	// 作品页那条同样不能被环拖挂：三环成员与拐进环的尾巴都只显示「没有归宿」。
	for _, s := range []string{"it-merge-p", "it-merge-t"} {
		revID := mergeTestRevision(t, pool, ids[s])
		revRows, err := q.ListRevisionModels(ctx, revID)
		if err != nil {
			t.Fatalf("环下 ListRevisionModels(%s) 报错: %v", s, err)
		}
		if len(revRows) != 1 {
			t.Fatalf("ListRevisionModels(%s) 在环下返回 %d 行，想要 1 行", s, len(revRows))
		}
		if revRows[0].MergedIntoSlug != "" {
			t.Errorf("ListRevisionModels(%s)：环下给出归宿 %q，应当是空", s, revRows[0].MergedIntoSlug)
		}
	}

	// 链尾悬空：u 仍指向存在的 v，v 自己归宿为空——四条路径同一口径，整条链不会一起消失。
	if got, ok := mergedSlug(rows, "it-merge-u"); !ok || got != "it-merge-v" {
		t.Errorf("链尾悬空：ListModels 里 u 的归宿是 %q（找到=%v），想要 it-merge-v", got, ok)
	}
	if got, ok := mergedSlug(rows, "it-merge-v"); !ok || got != "" {
		t.Errorf("链尾悬空：ListModels 里 v 的归宿是 %q（找到=%v），应当是空", got, ok)
	}
	if row, err := q.GetModelBySlug(ctx, "it-merge-u"); err != nil {
		t.Fatalf("链尾悬空下 GetModelBySlug(u) 报错: %v", err)
	} else if row.MergedIntoSlug != "it-merge-v" {
		t.Errorf("链尾悬空：GetModelBySlug(u) 的归宿是 %q，想要 it-merge-v", row.MergedIntoSlug)
	}
	if row, err := q.GetModelBySlug(ctx, "it-merge-v"); err != nil {
		t.Fatalf("链尾悬空下 GetModelBySlug(v) 报错: %v", err)
	} else if row.MergedIntoSlug != "" {
		t.Errorf("链尾悬空：GetModelBySlug(v) 的归宿是 %q，应当是空", row.MergedIntoSlug)
	}
	if byID, err := q.GetModelByID(ctx, ids["it-merge-u"]); err != nil {
		t.Fatalf("链尾悬空下 GetModelByID(u) 报错: %v", err)
	} else if byID.MergedIntoSlug != "it-merge-v" {
		t.Errorf("链尾悬空：GetModelByID(u) 的归宿是 %q，想要 it-merge-v", byID.MergedIntoSlug)
	}
	if byID, err := q.GetModelByID(ctx, ids["it-merge-v"]); err != nil {
		t.Fatalf("链尾悬空下 GetModelByID(v) 报错: %v", err)
	} else if byID.MergedIntoSlug != "" {
		t.Errorf("链尾悬空：GetModelByID(v) 的归宿是 %q，应当是空", byID.MergedIntoSlug)
	}
	revID := mergeTestRevision(t, pool, ids["it-merge-u"])
	revRows, err := q.ListRevisionModels(ctx, revID)
	if err != nil {
		t.Fatalf("链尾悬空下 ListRevisionModels 报错: %v", err)
	}
	if len(revRows) != 1 {
		t.Fatalf("链尾悬空下 ListRevisionModels 返回 %d 行，想要 1 行", len(revRows))
	}
	if revRows[0].MergedIntoSlug != "it-merge-v" {
		t.Errorf("链尾悬空：ListRevisionModels(u) 的归宿是 %q，想要 it-merge-v", revRows[0].MergedIntoSlug)
	}
	vRevID := mergeTestRevision(t, pool, ids["it-merge-v"])
	vRows, err := q.ListRevisionModels(ctx, vRevID)
	if err != nil {
		t.Fatalf("链尾悬空下 ListRevisionModels(v) 报错: %v", err)
	}
	if len(vRows) != 1 {
		t.Fatalf("链尾悬空下 ListRevisionModels(v) 返回 %d 行，想要 1 行", len(vRows))
	}
	if vRows[0].MergedIntoSlug != "" {
		t.Errorf("链尾悬空：ListRevisionModels(v) 的归宿是 %q，应当是空", vRows[0].MergedIntoSlug)
	}

	// ListModels 是四条读路径里唯一返回多行的，逐行「恰好一次」在这里断言；
	// 单行接口由主键/唯一键保证，ListRevisionModels 在上面按用例断言「恰好 1 行」。
	counts := map[string]int{}
	for _, r := range rows {
		counts[r.Slug]++
	}
	for _, s := range slugs {
		if counts[s] != 1 {
			t.Errorf("环或悬空指针下 ListModels 里 %s 出现了 %d 次，应当恰好一次", s, counts[s])
		}
	}
}

func TestIntegrationReadPathsStopAtDepthLimit(t *testing.T) {
	pool := checkpointTestPool(t)
	ctx := context.Background()
	q := store.New(pool)

	// 0→1→…→22：从 n00 数是 22 跳，超过 20 层上限，应当停在 n20。
	const hops = 22
	slugs := make([]string, 0, hops+1)
	ids := make([]int64, 0, hops+1)
	for i := 0; i <= hops; i++ {
		slugs = append(slugs, fmt.Sprintf("it-merge-n%02d", i))
	}
	t.Cleanup(func() { cleanupCheckpointTest(t, pool, slugs) })
	for _, slug := range slugs {
		ids = append(ids, mergeTestModel(t, pool, slug))
	}
	for i := 0; i < hops; i++ {
		mergeModels(t, pool, ids[i], ids[i+1])
	}

	rows, err := q.ListModels(ctx, pgtype.Text{})
	if err != nil {
		t.Fatalf("ListModels 失败: %v", err)
	}
	// 每行恰好一次：去掉递归上限后视图会同时吐出深度 20/21/22 三个终点行，
	// 只断言「取到的第一个是 n20」会被行的随机顺序骗过。
	counts := map[string]int{}
	for _, r := range rows {
		counts[r.Slug]++
	}
	for _, s := range []string{"it-merge-n00", "it-merge-n05", "it-merge-n22"} {
		if counts[s] != 1 {
			t.Errorf("22 跳的链里 %s 在 ListModels 出现了 %d 次，应当恰好一次", s, counts[s])
		}
	}

	got, ok := mergedSlug(rows, "it-merge-n00")
	if !ok {
		t.Fatalf("ListModels 里没有 it-merge-n00")
	}
	if got != "it-merge-n20" {
		t.Errorf("22 跳的链：n00 的归宿是 %q，想要第 20 层的 it-merge-n20", got)
	}
	// 同一张表里 17 跳的 n05 没到上限，归宿应当一路走到链尾 n22。
	if got05, ok := mergedSlug(rows, "it-merge-n05"); !ok || got05 != "it-merge-n22" {
		t.Errorf("n05 的归宿是 %q（找到=%v），想要 it-merge-n22", got05, ok)
	}
	if got22, ok := mergedSlug(rows, "it-merge-n22"); !ok || got22 != "" {
		t.Errorf("链尾 n22 的归宿是 %q（找到=%v），应当是空", got22, ok)
	}
}

func TestIntegrationReadPathsStopLongCycleAtDepthLimit(t *testing.T) {
	pool := checkpointTestPool(t)
	ctx := context.Background()
	q := store.New(pool)

	// 21 个成员首尾相接：环长 21 > 20 层上限，走不到重复节点，只能按上限停住。
	// 这是「20 层为界」的已知代价（见 0030 迁移注释）：每个成员都指向第 20 跳后的
	// 那个成员——页面显示的是一条环内链接，而不是「没有归宿」。
	const ring = 21
	slugs := make([]string, 0, ring)
	ids := make([]int64, 0, ring)
	modelIDs := make(map[string]int64, ring)
	for i := 0; i < ring; i++ {
		slugs = append(slugs, fmt.Sprintf("it-merge-c%02d", i))
	}
	t.Cleanup(func() { cleanupCheckpointTest(t, pool, slugs) })
	for _, slug := range slugs {
		id := mergeTestModel(t, pool, slug)
		ids = append(ids, id)
		modelIDs[slug] = id
	}
	for i := 0; i < ring; i++ {
		mergeModels(t, pool, ids[i], ids[(i+1)%ring])
	}

	rows, err := q.ListModels(ctx, pgtype.Text{})
	if err != nil {
		t.Fatalf("21 环下 ListModels 报错: %v", err)
	}
	counts := map[string]int{}
	for _, r := range rows {
		counts[r.Slug]++
	}
	for _, s := range slugs {
		if counts[s] != 1 {
			t.Errorf("21 环里 %s 在 ListModels 出现了 %d 次，应当恰好一次", s, counts[s])
		}
	}
	// 环长 21 不整除 20，所以 c00 走 20 跳落在 c20、c01 落在 c00，都没有回到起点。
	// 四条读路径都断言：展示侧同口径，不能只在目录列表里对。
	for _, tc := range []struct{ slug, want string }{
		{"it-merge-c00", "it-merge-c20"},
		{"it-merge-c01", "it-merge-c00"},
	} {
		if got, ok := mergedSlug(rows, tc.slug); !ok || got != tc.want {
			t.Errorf("21 环：ListModels 里 %s 的归宿是 %q（找到=%v），想要 %q", tc.slug, got, ok, tc.want)
		}
		if row, err := q.GetModelBySlug(ctx, tc.slug); err != nil {
			t.Fatalf("21 环下 GetModelBySlug(%s) 报错: %v", tc.slug, err)
		} else if row.MergedIntoSlug != tc.want {
			t.Errorf("21 环：GetModelBySlug(%s) 的归宿是 %q，想要 %q", tc.slug, row.MergedIntoSlug, tc.want)
		}
		if byID, err := q.GetModelByID(ctx, modelIDs[tc.slug]); err != nil {
			t.Fatalf("21 环下 GetModelByID(%s) 报错: %v", tc.slug, err)
		} else if byID.MergedIntoSlug != tc.want {
			t.Errorf("21 环：GetModelByID(%s) 的归宿是 %q，想要 %q", tc.slug, byID.MergedIntoSlug, tc.want)
		}
		revID := mergeTestRevision(t, pool, modelIDs[tc.slug])
		revRows, err := q.ListRevisionModels(ctx, revID)
		if err != nil {
			t.Fatalf("21 环下 ListRevisionModels(%s) 报错: %v", tc.slug, err)
		}
		if len(revRows) != 1 {
			t.Fatalf("21 环下 ListRevisionModels(%s) 返回 %d 行，想要 1 行", tc.slug, len(revRows))
		}
		if revRows[0].MergedIntoSlug != tc.want {
			t.Errorf("21 环：ListRevisionModels(%s) 的归宿是 %q，想要 %q", tc.slug, revRows[0].MergedIntoSlug, tc.want)
		}
	}
}
