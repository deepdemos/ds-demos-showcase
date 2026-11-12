package demo

import (
	"context"
	"regexp"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// 模型指称的解析规则：**允许歧义，同 rank 取小的 `model_id`**。
//
// 这条规则原先只是 `ResolveModelRefs` 里的一句注释，没有任何用例压着；而库里那条
// 「同一个写法只能指一个模型」的约束（`model_aliases` 的单列主键）从来没有真正
// 保证过它——解析用 `lower(a.alias)` 匹配，主键却大小写敏感。迁移 0027 把主键改成
// `(alias, model_id)` 之后，「同一个写法指两个模型」变成可写入的事实，这条规则也就
// 从「注释里的一厢情愿」变成了唯一的裁决者。所以这里补上用例。

// mkAliasTestModel 建一个模型并返回 id。名字与 slug 都由调用方给全，便于断言。
func mkAliasTestModel(t *testing.T, pool *pgxpool.Pool, slug, name string) int64 {
	t.Helper()
	var id int64
	if err := pool.QueryRow(context.Background(),
		`INSERT INTO models (slug, name, vendor, status) VALUES ($1, $2, '', 'active')
		 ON CONFLICT (slug) DO UPDATE SET name = excluded.name
		 RETURNING id`, slug, name).Scan(&id); err != nil {
		t.Fatalf("插入模型 %s 失败: %v", slug, err)
	}
	return id
}

// cleanupAliasTest 先删别名再删模型：`model_aliases.model_id` **没有外键**
// （见 docs/已知松点.md），删模型不会带走别名行，必须显式删。
func cleanupAliasTest(t *testing.T, pool *pgxpool.Pool, aliases []string, slugs []string) {
	t.Helper()
	ctx := context.Background()
	for _, a := range aliases {
		if _, err := pool.Exec(ctx, `DELETE FROM model_aliases WHERE alias = $1`, a); err != nil {
			t.Logf("清理别名 %s 失败: %v", a, err)
		}
	}
	for _, s := range slugs {
		if _, err := pool.Exec(ctx, `DELETE FROM models WHERE slug = $1`, s); err != nil {
			t.Logf("清理模型 %s 失败: %v", s, err)
		}
	}
}

// TestIntegrationResolveModelRefsAmbiguousAliasPicksLowestID 钉住「同一个写法指两个
// 模型时，解析稳定取小的 id」。
//
// 为什么重要：别名来自作者手填，撞名是常态。规则若不稳定，作者两次打开详情页可能看到
// 两个不同的模型，而他什么都没改。
func TestIntegrationResolveModelRefsAmbiguousAliasPicksLowestID(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	q := store.New(pool)

	first := mkAliasTestModel(t, pool, "it-amb-alias-a", "歧义别名模型甲")
	second := mkAliasTestModel(t, pool, "it-amb-alias-b", "歧义别名模型乙")
	t.Cleanup(func() {
		cleanupAliasTest(t, pool, []string{"同名写法"}, []string{"it-amb-alias-a", "it-amb-alias-b"})
	})

	// 同一个别名挂到两个模型上——迁移 0027 之后这是合法的写入。
	if _, err := pool.Exec(ctx,
		`INSERT INTO model_aliases (alias, model_id) VALUES ('同名写法', $1), ('同名写法', $2)
		 ON CONFLICT (alias, model_id) DO NOTHING`, first, second); err != nil {
		t.Fatalf("造歧义别名失败: %v", err)
	}

	var rows int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM model_aliases WHERE alias = '同名写法'`).Scan(&rows); err != nil {
		t.Fatalf("数别名行失败: %v", err)
	}
	// 先证明歧义真的在库里，而不是被主键挡掉了一条——否则下面的断言等于没验。
	if rows != 2 {
		t.Fatalf("别名行数 = %d，想要 2（歧义应当写得进去）", rows)
	}

	want := first
	if second < first {
		want = second
	}
	// 连查 3 次：规则要**稳定**，不是「这一次刚好取到小的」。
	for i := 0; i < 3; i++ {
		got, err := resolveModelRefs(ctx, q, []string{"同名写法"})
		if err != nil {
			t.Fatalf("第 %d 次解析失败: %v", i+1, err)
		}
		if len(got) != 1 || got[0] != want {
			t.Fatalf("第 %d 次解析 = %v，想要 [%d]（同 rank 取小的 model_id）", i+1, got, want)
		}
	}
}

// TestIntegrationResolveModelRefsRankBeatsID 钉住「先比 rank 再比 id」。
//
// 把规则简化成「永远取小 id」会在这里红：别名(rank 1) 赢展示名(rank 2)，即使它的 id 更大。
func TestIntegrationResolveModelRefsRankBeatsID(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	q := store.New(pool)

	// 先建的用「展示名」撞，后建的用「别名」撞，于是后建的 id 更大。
	byName := mkAliasTestModel(t, pool, "it-rank-name", "撞名")
	byAlias := mkAliasTestModel(t, pool, "it-rank-alias", "撞名二号")
	t.Cleanup(func() {
		cleanupAliasTest(t, pool, []string{"撞名"}, []string{"it-rank-name", "it-rank-alias"})
	})
	if byAlias <= byName {
		t.Fatalf("用例前提不成立：按别名胜出的 id %d 应当大于按名字胜出的 id %d", byAlias, byName)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO model_aliases (alias, model_id) VALUES ('撞名', $1)
		 ON CONFLICT (alias, model_id) DO NOTHING`, byAlias); err != nil {
		t.Fatalf("造别名失败: %v", err)
	}

	got, err := resolveModelRefs(ctx, q, []string{"撞名"})
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(got) != 1 || got[0] != byAlias {
		t.Fatalf("解析 = %v，想要 [%d]：别名 rank(1) 应当赢展示名 rank(2)，即使 id 更大", got, byAlias)
	}
}

// TestIntegrationInsertModelAliasIsIdempotentPerModel 钉住复合主键的语义：
// 同一对 (别名, 模型) 连插两次不报错，但也不会长出第二行。
//
// 这条是「作者在两次投稿里都写了同一个模型名」的日常路径：`InsertModelAlias` 必须
// 幂等，否则第二次投稿会撞主键失败。
func TestIntegrationInsertModelAliasIsIdempotentPerModel(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	q := store.New(pool)

	id := mkAliasTestModel(t, pool, "it-alias-idem", "幂等别名模型")
	t.Cleanup(func() {
		cleanupAliasTest(t, pool, []string{"幂等写法"}, []string{"it-alias-idem"})
	})

	for i := 0; i < 2; i++ {
		if err := q.InsertModelAlias(ctx, store.InsertModelAliasParams{Alias: "幂等写法", ModelID: id}); err != nil {
			t.Fatalf("第 %d 次记别名失败: %v", i+1, err)
		}
	}
	var rows int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM model_aliases WHERE alias = '幂等写法'`).Scan(&rows); err != nil {
		t.Fatalf("数别名行失败: %v", err)
	}
	if rows != 1 {
		t.Fatalf("别名行数 = %d，想要 1（同一对 (alias, model_id) 只该有一行）", rows)
	}
}

// cleanupRaceTest 按模型名清掉别名与模型行：`model_aliases.model_id` 没有外键
// （见 docs/已知松点.md），删模型不会带走别名行。
func cleanupRaceTest(t *testing.T, pool *pgxpool.Pool, names []string) {
	t.Helper()
	ctx := context.Background()
	for _, n := range names {
		if _, err := pool.Exec(ctx,
			`DELETE FROM model_aliases WHERE model_id IN (SELECT id FROM models WHERE name = $1)`, n); err != nil {
			t.Logf("清理别名（按模型名 %s）失败: %v", n, err)
		}
		if _, err := pool.Exec(ctx, `DELETE FROM models WHERE name = $1`, n); err != nil {
			t.Logf("清理模型（按名字 %s）失败: %v", n, err)
		}
	}
}

// TestIntegrationCreateCandidateModelRecoversWhenNameExists 钉住「并发撞名时后到的那次不许失败」。
//
// 形状：库里已经有同名的模型，调用方却没在 `ResolveModelRefs` 里查到它（真实的并发窗口是
// 「查的时候还没有、写的时候已经有了」）。`CreateCandidateModel` 是 `ON CONFLICT DO NOTHING`，
// 冲突时**不报 23505、只返回 0 行**，sqlc 的 `:one` 于是让 Scan 返回 `pgx.ErrNoRows`。
//
// 修之前：这条 ErrNoRows 落到 default 分支，作者看到「新建模型失败: no rows in result set」，
// 整次投稿失败——而他什么都没做错，只是和别人同时提到了同一个新模型名（issue #420）。
//
// 变异验证：把 `createCandidateModel` 的 `case` 里 `errors.Is(err, pgx.ErrNoRows)` 去掉，
// 这条必须红。
func TestIntegrationCreateCandidateModelRecoversWhenNameExists(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	q := store.New(pool)

	const name = "并发撞名模型甲"
	t.Cleanup(func() { cleanupRaceTest(t, pool, []string{name}) })
	cleanupRaceTest(t, pool, []string{name}) // 上一轮残留的兜底

	existing := mkAliasTestModel(t, pool, "it-race-existing-a", name)

	got, err := createCandidateModel(ctx, q, name)
	if err != nil {
		t.Fatalf("撞上已存在的名字时不该失败，实际: %v", err)
	}
	if got != existing {
		t.Fatalf("回查拿到 %d，想要已有的 %d（回查应当按名字命中同一行）", got, existing)
	}

	var rows int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM models WHERE name = $1`, name).Scan(&rows); err != nil {
		t.Fatalf("数模型行失败: %v", err)
	}
	if rows != 1 {
		t.Fatalf("同名模型有 %d 行，想要 1（撞名不该长出新行）", rows)
	}
}

// TestIntegrationCreateCandidateModelConcurrentSameName 是两个 goroutine 真的同时建同一个名字。
//
// 与上一条的区别：那条把「已经存在」这一步做成了确定的，这条让它自己去争。两条都要有——
// 前者可稳定复现，后者证明真实并发下的形状也是它（不靠时序假设）。
func TestIntegrationCreateCandidateModelConcurrentSameName(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	q := store.New(pool)

	const name = "并发撞名模型乙"
	t.Cleanup(func() { cleanupRaceTest(t, pool, []string{name}) })
	cleanupRaceTest(t, pool, []string{name})

	const n = 2
	ids := make([]int64, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start // 一起放行，尽量重叠
			ids[i], errs[i] = createCandidateModel(ctx, q, name)
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("第 %d 个并发请求失败了: %v（并发撞名不是作者的错）", i+1, err)
		}
	}
	if ids[0] != ids[1] {
		t.Fatalf("两个请求拿到不同的 id %v，想要同一个（名字唯一约束保证只有一行）", ids)
	}

	var rows int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM models WHERE name = $1`, name).Scan(&rows); err != nil {
		t.Fatalf("数模型行失败: %v", err)
	}
	if rows != 1 {
		t.Fatalf("同名模型有 %d 行，想要 1", rows)
	}
	var aliases int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM model_aliases WHERE alias = $1`, name).Scan(&aliases); err != nil {
		t.Fatalf("数别名行失败: %v", err)
	}
	if aliases != 1 {
		t.Fatalf("别名有 %d 行，想要 1（同一对 (alias, model_id) 只该有一行）", aliases)
	}
}

// waitForBlockedInsert 等到有别的连接卡在锁上——说明另一个调用方已经走进 INSERT、
// 正等这笔未提交的事务放开唯一索引。等不到就是用例前提不成立：直接失败，而不是继续跑
// 出一个「碰巧过了」的结果。
func waitForBlockedInsert(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var n int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity
			WHERE datname = current_database() AND wait_event_type = 'Lock'
			  AND query ILIKE '%INSERT INTO models%'`).Scan(&n); err != nil {
			t.Fatalf("查等锁连接数失败: %v", err)
		}
		if n > 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("另一个调用方 5 秒内没有卡在唯一索引上，用例前提不成立")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// slugRetrySuffix 是「撞车之后该拿到的 slug」的形状：基座 + 8 位十六进制
// （randomSuffix(4) 用 hex.EncodeToString，4 字节变 8 个字符）。
var slugRetrySuffix = regexp.MustCompile(`^qwen-2-5-[0-9a-f]{8}$`)

// TestIntegrationCreateCandidateModelRetriesWhenSlugTaken 钉住「slug 撞车时换一个 slug 重来」。
//
// 形状：两个**不同**的名字折出同一个基座（「Qwen-2.5」与「Qwen 2.5」都折成 qwen-2-5）。
// uniqueModelSlug 是「先查再插」，两边都可能查到「没被占用」，后插的那条撞 models.slug
// 的唯一约束 → ON CONFLICT DO NOTHING 返回 0 行 → 按名字回查也查不到（名字确实不同）
// → 修之前作者会看到「回查同名模型失败: no rows in result set」（issue #442）。
//
// 这条用例不靠调度时序：先用一个**未提交**的事务占住 slug，另一个调用方查
// ModelSlugTaken 时还看不见它，走到 INSERT 必然卡在唯一索引上；等它真的卡住了再提交，
// 撞车是造出来的，不是赌出来的。
func TestIntegrationCreateCandidateModelRetriesWhenSlugTaken(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	q := store.New(pool)

	const name = "Qwen-2.5"  // 折成 qwen-2-5
	const rival = "Qwen 2.5" // 同一个基座，但名字不同
	t.Cleanup(func() { cleanupRaceTest(t, pool, []string{name, rival}) })
	cleanupRaceTest(t, pool, []string{name, rival})

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("开事务失败: %v", err)
	}
	committed := false
	t.Cleanup(func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	})
	if _, err := tx.Exec(ctx,
		`INSERT INTO models (slug, name, vendor, status) VALUES ('qwen-2-5', $1, '', 'candidate')`, rival); err != nil {
		t.Fatalf("占住 slug 失败: %v", err)
	}

	type result struct {
		id  int64
		err error
	}
	done := make(chan result, 1)
	go func() {
		id, err := createCandidateModel(ctx, q, name)
		done <- result{id: id, err: err}
	}()

	waitForBlockedInsert(t, pool)

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("提交占位事务失败: %v", err)
	}
	committed = true

	got := <-done
	if got.err != nil {
		t.Fatalf("slug 撞车不该让作者失败，实际: %v", got.err)
	}

	var slug, storedName string
	if err := pool.QueryRow(ctx, `SELECT slug, name FROM models WHERE id = $1`, got.id).Scan(&slug, &storedName); err != nil {
		t.Fatalf("回查新模型失败: %v", err)
	}
	if storedName != name {
		t.Fatalf("落库名字 = %q，想要 %q", storedName, name)
	}
	if !slugRetrySuffix.MatchString(slug) {
		t.Fatalf("slug = %q，想要 qwen-2-5-<8 位十六进制>（撞车后应当换一个带后缀的 slug）", slug)
	}

	// 占位那条不该被顶掉：撞车只该让后到者换 slug，不该动别人。
	var rivalSlug string
	if err := pool.QueryRow(ctx, `SELECT slug FROM models WHERE name = $1`, rival).Scan(&rivalSlug); err != nil {
		t.Fatalf("占位模型不见了: %v", err)
	}
	if rivalSlug != "qwen-2-5" {
		t.Fatalf("占位模型的 slug = %q，想要 qwen-2-5（不该被顶掉）", rivalSlug)
	}

	// 别名要记在这次的新模型上：重试不能把别名漏掉。
	var aliasModel int64
	if err := pool.QueryRow(ctx, `SELECT model_id FROM model_aliases WHERE alias = $1`, name).Scan(&aliasModel); err != nil {
		t.Fatalf("别名没记上: %v", err)
	}
	if aliasModel != got.id {
		t.Fatalf("别名指向 %d，想要 %d", aliasModel, got.id)
	}
}
