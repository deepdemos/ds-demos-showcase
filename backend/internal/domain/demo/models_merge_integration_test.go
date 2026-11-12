package demo

// 模型合并链的解析：A→B→C 要跟到尽头，环与悬空指针要有兜底。
//
// 与 `models_integration_test.go` 同包：那句文件里是别名与候选模型的规则，这里只管
// `merged_into_id` 这条指针。分开是为了两个文件都不越过 500 行的告警线
// （`scripts/check-file-size.mjs` 在 800 行才失败，但 500 行就该拆了）。

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// 合并链：解析要跟到尽头，不能停在中转站。
//
// 旧实现沿 `merged_into_id` 只走一跳（`LEFT JOIN models t ON t.id = m.merged_into_id`）。A→B→C
// 这种两跳链上，按 A 的写法解析会停在 B；而 B 自己也被合并掉了，于是「按名字解析」与
// 「按 id 查」给出两个不同的归宿。用例把两跳都钉住，顺便确认旧 id 仍然查得到（#418 G02）。
func TestIntegrationResolveModelRefsFollowsMergeChainToTheEnd(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	q := store.New(pool)

	chainHead := mkAliasTestModel(t, pool, "it-chain-a", "链首甲")
	middle := mkAliasTestModel(t, pool, "it-chain-b", "链中乙")
	tail := mkAliasTestModel(t, pool, "it-chain-c", "链尾丙")
	t.Cleanup(func() {
		clearMerges(t, pool, chainHead, middle)
		cleanupAliasTest(t, pool, nil, []string{"it-chain-a", "it-chain-b", "it-chain-c"})
	})

	// A→B、B→C，两跳。
	setMerged(t, pool, chainHead, middle)
	setMerged(t, pool, middle, tail)

	for _, ref := range []string{"it-chain-a", "链首甲", "it-chain-b", "链中乙"} {
		got, err := resolveModelRefs(ctx, q, []string{ref})
		if err != nil {
			t.Fatalf("解析 %q 失败: %v", ref, err)
		}
		if len(got) != 1 || got[0] != tail {
			t.Fatalf("解析 %q = %v，想要 [%d]（跟到链尾；停在中间站说明只走了一跳）", ref, got, tail)
		}
		if len(got) == 1 && got[0] == middle {
			t.Fatalf("解析 %q 停在中间站 %d，应当跟到 %d", ref, middle, tail)
		}
	}

	// 旧 id 仍可查（#418 G02）：合并只影响「按名字解析」，不改历史行。
	if m, err := q.GetModelByID(ctx, chainHead); err != nil {
		t.Fatalf("按 id 查已合并模型失败: %v", err)
	} else if m.ID != chainHead {
		t.Fatalf("按 id 查到 %d，想要 %d", m.ID, chainHead)
	}
}

// 环：A→B→A。必须**有界**返回，不能无限递归把库拖住。
//
// 库里没有这个形状的写入口（合并走治理动作），但 `merged_into_id` 没有外键、也没有环检测
// 约束，一次人工 UPDATE 就能造出来。这里钉的是「解析在环上仍然给出一行、并且每次都一样」。
func TestIntegrationResolveModelRefsSurvivesCycle(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	q := store.New(pool)

	first := mkAliasTestModel(t, pool, "it-cycle-a", "环甲")
	second := mkAliasTestModel(t, pool, "it-cycle-b", "环乙")
	t.Cleanup(func() {
		clearMerges(t, pool, first, second)
		cleanupAliasTest(t, pool, nil, []string{"it-cycle-a", "it-cycle-b"})
	})

	setMerged(t, pool, first, second)
	setMerged(t, pool, second, first)

	// 起点的 depth 是 0，环上偶数层回到 first，所以 20 层上限下的终点**恒为 first**——
	// 不是「同 rank 取小 id」（那条规则只在同一 rank 有多行候选时才用得上）。
	// 这里断言的是「有界、且每次一样」，与两个 id 谁大谁小无关。
	want := first
	for i := 0; i < 3; i++ {
		done := make(chan []int64, 1)
		errCh := make(chan error, 1)
		go func() {
			got, err := resolveModelRefs(ctx, q, []string{"it-cycle-a"})
			if err != nil {
				errCh <- err
				return
			}
			done <- got
		}()
		select {
		case got := <-done:
			if len(got) != 1 || got[0] != want {
				t.Fatalf("第 %d 次解析环上的写入 = %v，想要 [%d]", i+1, got, want)
			}
		case err := <-errCh:
			t.Fatalf("第 %d 次解析环失败: %v", i+1, err)
		case <-time.After(5 * time.Second):
			t.Fatalf("第 %d 次解析在环上超过 5 秒——上限没起作用", i+1)
		}
	}
}

// 指针悬空：归宿被硬删了（`models.merged_into_id` 没有外键），解析要退回被合并那个 id，
// 不能解析成空——否则作者手里的旧名字会重新变成一条待建的候选模型。
func TestIntegrationResolveModelRefsFallsBackWhenTargetMissing(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	q := store.New(pool)

	row := mkAliasTestModel(t, pool, "it-dangle-a", "悬空甲")
	t.Cleanup(func() {
		clearMerges(t, pool, row)
		cleanupAliasTest(t, pool, nil, []string{"it-dangle-a"})
	})

	// 指向一个不存在的 id：没有外键，写得进去。
	if _, err := pool.Exec(ctx,
		"UPDATE models SET status = 'deprecated', merged_into_id = $2 WHERE id = $1",
		row, row+100000); err != nil {
		t.Fatalf("造悬空指针失败: %v", err)
	}

	got, err := resolveModelRefs(ctx, q, []string{"it-dangle-a"})
	if err != nil {
		t.Fatalf("解析悬空指针失败: %v", err)
	}
	if len(got) != 1 || got[0] != row {
		t.Fatalf("解析悬空指针 = %v，想要 [%d]（退回被合并的那个 id）", got, row)
	}
}

// 自指在库里就被挡住（`ck_models_no_self`），不是靠解析层兜底。
func TestIntegrationModelSelfMergeIsRejectedBySchema(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	row := mkAliasTestModel(t, pool, "it-self-a", "自指甲")
	t.Cleanup(func() {
		cleanupAliasTest(t, pool, nil, []string{"it-self-a"})
	})

	if _, err := pool.Exec(ctx,
		"UPDATE models SET merged_into_id = id WHERE id = $1", row); err == nil {
		t.Fatalf("自指写进去了：ck_models_no_self 没起作用")
	}
}

// 超过上限的长链：20 层就停，不等于「跟到尽头」。
//
// 为什么要有：递归项带 `depth < 20`，那是兜住环的手段，但它对**非环的长链**同样生效。
// 真实数据里合并不该有 20 跳（合并是人工动作，链一般一跳），可既然上限是写死的，就要有
// 用例说清「超过 20 跳时返回的是第 20 层那个模型，而不是链尾」——否则改上限的人只能靠
// 读 SQL 猜影响面，或者以为这条路径只有环会碰到。
func TestIntegrationResolveModelRefsStopsAtDepthLimit(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	q := store.New(pool)

	const hops = 24 // 24 跳 = 25 个模型；起点 depth 是 0，所以停在第 20 个模型上
	ids := make([]int64, hops+1)
	slugs := make([]string, hops+1)
	for i := range ids {
		slugs[i] = fmt.Sprintf("it-long-%02d", i)
		ids[i] = mkAliasTestModel(t, pool, slugs[i], fmt.Sprintf("长链%02d", i))
	}
	t.Cleanup(func() {
		clearMerges(t, pool, ids[:len(ids)-1]...)
		cleanupAliasTest(t, pool, nil, slugs)
	})

	for i := 0; i < hops; i++ {
		setMerged(t, pool, ids[i], ids[i+1])
	}

	got, err := resolveModelRefs(ctx, q, []string{slugs[0]})
	if err != nil {
		t.Fatalf("解析长链失败: %v", err)
	}
	if len(got) != 1 || got[0] != ids[20] {
		t.Fatalf("解析 %s = %v，想要 [%d]（第 20 层停住）；链尾是 %d",
			slugs[0], got, ids[20], ids[hops])
	}
}

// setMerged 把 from 标成已合并到 into。
func setMerged(t *testing.T, pool *pgxpool.Pool, from, into int64) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		"UPDATE models SET status = 'deprecated', merged_into_id = $2 WHERE id = $1",
		from, into); err != nil {
		t.Fatalf("设置合并 %d → %d 失败: %v", from, into, err)
	}
}

// clearMerges 清掉合并指针并恢复 status——清理必须走在删行之前，否则删的是别人正在用的目标。
func clearMerges(t *testing.T, pool *pgxpool.Pool, ids ...int64) {
	t.Helper()
	for _, id := range ids {
		if _, err := pool.Exec(context.Background(),
			"UPDATE models SET merged_into_id = NULL, status = 'active' WHERE id = $1", id); err != nil {
			t.Logf("清理合并指针 %d 失败: %v", id, err)
		}
	}
}
