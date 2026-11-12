package demo

import (
	"context"
	"testing"

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
