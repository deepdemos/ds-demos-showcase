package model

// 0029 给模型加了「版本」（checkpoint）。这里是它的库层用例。
//
// 为什么要有：版本是「我用的是十月中旬那版」这句话的落点，而这句话一旦写下来就不能
// 被悄悄改写——所以 checkpoint 被引用后必须删不掉（RESTRICT），同一模型下同名版本只能
// 有一行（否则「作品挂在 1017 上」指哪一行全看查询顺序），日期不明要能表达（NULL 而不是
// 编一个日期）。这三条都是库层能钉住、上层代码钉不住的，所以用例放在这一层。
//
// 与 catalog 那条对账用例不同，这里不打表结构，只走真实写入路径：INSERT / 回查 / 列表 /
// 被引用时删不掉。表结构的静态部分由 verify-schema 与 check-schema 管。

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// checkpointTestPool 连测试库：与 catalog_payload_integration_test.go 同一约束——没设
// TEST_DATABASE_URL 就跳过，CI 里一定会设。
func checkpointTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL 未设置，跳过集成测试（CI 里会设）")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("连接测试库失败: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// mkCheckpointTestModel 建一个模型并返回 id。slug 与名字都由调用方给全，便于断言。
func mkCheckpointTestModel(t *testing.T, pool *pgxpool.Pool, slug, name string) int64 {
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

// cleanupCheckpointTest 先删调用行与版本、再删模型：两张表的引用都是 RESTRICT，
// 顺序反了会删不动（这正是用例要证明的行为，不该在收尾时撞上）。
func cleanupCheckpointTest(t *testing.T, pool *pgxpool.Pool, slugs []string) {
	t.Helper()
	ctx := context.Background()
	for _, s := range slugs {
		if _, err := pool.Exec(ctx, `DELETE FROM model_offerings WHERE model_id IN (SELECT id FROM models WHERE slug = $1)`, s); err != nil {
			t.Logf("清理调用行 %s 失败: %v", s, err)
		}
		if _, err := pool.Exec(ctx, `DELETE FROM model_checkpoints WHERE model_id IN (SELECT id FROM models WHERE slug = $1)`, s); err != nil {
			t.Logf("清理版本 %s 失败: %v", s, err)
		}
		if _, err := pool.Exec(ctx, `DELETE FROM models WHERE slug = $1`, s); err != nil {
			t.Logf("清理模型 %s 失败: %v", s, err)
		}
	}
}

// mkCheckpointTestDemo 建一个作品与它的一版，返回 revision id。
//
// 作品侧要插两行才能挂模型声明：demo_revision_models 指向 demo_revisions，而 demo_revisions
// 又指向 demos——模型声明不是直接挂在作品上的。
func mkCheckpointTestDemo(t *testing.T, pool *pgxpool.Pool, slug string) int64 {
	t.Helper()
	ctx := context.Background()
	var demoID int64
	if err := pool.QueryRow(ctx,
		`INSERT INTO demos (slug) VALUES ($1) RETURNING id`, slug).Scan(&demoID); err != nil {
		t.Fatalf("插入作品 %s 失败: %v", slug, err)
	}
	var revID int64
	if err := pool.QueryRow(ctx,
		`INSERT INTO demo_revisions (demo_id, rev_no) VALUES ($1, 1) RETURNING id`, demoID).Scan(&revID); err != nil {
		t.Fatalf("插入作品版本失败: %v", err)
	}
	return revID
}

// cleanupCheckpointDemo 按 slug 删作品：版本与模型声明由外键 CASCADE 带走。
//
// 它必须比 cleanupCheckpointTest 先跑（t.Cleanup 是后进先出）：作品里的模型声明对 models
// 与 model_checkpoints 都是 RESTRICT，先删模型会删不动。
func cleanupCheckpointDemo(t *testing.T, pool *pgxpool.Pool, slug string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `DELETE FROM demos WHERE slug = $1`, slug); err != nil {
		t.Logf("清理作品 %s 失败: %v", slug, err)
	}
}

func checkpointDate(y int, m time.Month, d int) pgtype.Date {
	return pgtype.Date{Time: time.Date(y, m, d, 0, 0, 0, 0, time.UTC), Valid: true}
}

// TestIntegrationInsertModelCheckpointIsIdempotent 钉住「同一个版本登记两次无害」。
//
// 为什么重要：版本会被反复登记——同一份上游目录同步两次、作者改一次作品又提交一次。
// 若第二次报错，调用方就得先查再插，而那一段查插之间又是新的竞态。这里让冲突变成
// 「不返回行」，调用方据此回查（与 CreateCandidateModel 同一套路）。
func TestIntegrationInsertModelCheckpointIsIdempotent(t *testing.T) {
	pool := checkpointTestPool(t)
	ctx := context.Background()
	q := store.New(pool)

	modelID := mkCheckpointTestModel(t, pool, "it-ck-idem-a", "版本幂等模型甲")
	otherID := mkCheckpointTestModel(t, pool, "it-ck-idem-b", "版本幂等模型乙")
	t.Cleanup(func() { cleanupCheckpointTest(t, pool, []string{"it-ck-idem-a", "it-ck-idem-b"}) })

	first, err := q.InsertModelCheckpoint(ctx, store.InsertModelCheckpointParams{
		ModelID:    modelID,
		Label:      "1017",
		ReleasedOn: checkpointDate(2026, time.October, 17),
		Notes:      "十月中旬那版",
	})
	if err != nil {
		t.Fatalf("第一次登记版本失败: %v", err)
	}

	// 第二次同样的 (model_id, label)：ON CONFLICT DO NOTHING 不返回行。
	if _, err := q.InsertModelCheckpoint(ctx, store.InsertModelCheckpointParams{
		ModelID:    modelID,
		Label:      "1017",
		ReleasedOn: checkpointDate(2026, time.October, 17),
		Notes:      "十月中旬那版",
	}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("同一个版本登记两次应当返回 pgx.ErrNoRows（冲突即空），实际 err=%v", err)
	}

	found, err := q.FindModelCheckpoint(ctx, store.FindModelCheckpointParams{ModelID: modelID, Label: "1017"})
	if err != nil {
		t.Fatalf("回查版本失败: %v", err)
	}
	if found != first {
		t.Fatalf("回查到的版本 id 应当是第一次那行的 %d，实际 %d", first, found)
	}

	// 不同模型下的同名版本互不相干：唯一键是 (model_id, label)，不是 label。
	other, err := q.InsertModelCheckpoint(ctx, store.InsertModelCheckpointParams{
		ModelID:    otherID,
		Label:      "1017",
		ReleasedOn: pgtype.Date{},
		Notes:      "",
	})
	if err != nil {
		t.Fatalf("另一个模型登记同名版本应当成功: %v", err)
	}
	if other == first {
		t.Fatalf("两个模型的同名版本不应当是同一行（都拿到 id %d）", first)
	}

	// 按 model_id 数，不按 label 数：库里别的行也可能叫 1017（这是别的用例/探针的地盘）。
	var count int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM model_checkpoints WHERE model_id IN ($1, $2) AND label = '1017'`, modelID, otherID).
		Scan(&count); err != nil {
		t.Fatalf("数版本行失败: %v", err)
	}
	if count != 2 {
		t.Fatalf("同一个 label 挂了两个模型，应当两行，实际 %d 行", count)
	}
}

// TestIntegrationListModelCheckpointsPutsUndatedLast 钉住列表顺序：新的在前、日期不明垫底。
//
// 为什么重要：Postgres 的 DESC 默认是 NULLS FIRST，漏写 NULLS LAST 会让「日期不明」顶到
// 最前面，看起来像「最新的那版不知道什么时候发的」——这正是给人看的地方最容易出的错。
func TestIntegrationListModelCheckpointsPutsUndatedLast(t *testing.T) {
	pool := checkpointTestPool(t)
	ctx := context.Background()
	q := store.New(pool)

	modelID := mkCheckpointTestModel(t, pool, "it-ck-order", "版本排序模型")
	t.Cleanup(func() { cleanupCheckpointTest(t, pool, []string{"it-ck-order"}) })

	for _, arg := range []store.InsertModelCheckpointParams{
		{ModelID: modelID, Label: "preview", ReleasedOn: pgtype.Date{}, Notes: "日期不明"},
		{ModelID: modelID, Label: "1017", ReleasedOn: checkpointDate(2026, time.October, 17)},
		{ModelID: modelID, Label: "0917", ReleasedOn: checkpointDate(2026, time.September, 17)},
	} {
		if _, err := q.InsertModelCheckpoint(ctx, arg); err != nil {
			t.Fatalf("登记版本 %s 失败: %v", arg.Label, err)
		}
	}

	got, err := q.ListModelCheckpoints(ctx, modelID)
	if err != nil {
		t.Fatalf("列版本失败: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("应当列出 3 个版本，实际 %d 个", len(got))
	}
	want := []string{"1017", "0917", "preview"}
	for i, w := range want {
		if got[i].Label != w {
			t.Fatalf("第 %d 个版本应当是 %q，实际 %q（完整顺序 %v）", i, w, got[i].Label, labelsOf(got))
		}
	}
	if got[2].ReleasedOn.Valid {
		t.Fatalf("日期不明的版本在列表里应当仍是 NULL，实际 %v", got[2].ReleasedOn.Time)
	}
}

func labelsOf(rows []store.ModelCheckpoint) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.Label
	}
	return out
}

// TestIntegrationCheckpointInUseCannotBeDeleted 钉住「两处引用都是 RESTRICT」。
//
// 为什么重要：checkpoint 一旦被引用，它就是「当时用的是哪一版」这句话的一部分。
// 若能级联删掉，历史会被悄悄改写且没有任何痕迹——作品页会退回「只知道模型」的样子。
func TestIntegrationCheckpointInUseCannotBeDeleted(t *testing.T) {
	pool := checkpointTestPool(t)
	ctx := context.Background()
	q := store.New(pool)

	modelID := mkCheckpointTestModel(t, pool, "it-ck-ref", "版本引用模型")
	t.Cleanup(func() { cleanupCheckpointTest(t, pool, []string{"it-ck-ref"}) })

	ckID, err := q.InsertModelCheckpoint(ctx, store.InsertModelCheckpointParams{
		ModelID: modelID, Label: "1017", ReleasedOn: checkpointDate(2026, time.October, 17),
	})
	if err != nil {
		t.Fatalf("登记版本失败: %v", err)
	}

	// 调用行（端点上报的那张表）指到这一版。
	if _, err := pool.Exec(ctx, `
		INSERT INTO model_offerings (authority, provider_id, provider_name, external_id, model_id, checkpoint_id, pinning)
		VALUES ('endpoint', '', '', 'it-ck-ref-ext', $1, $2, 'exact')
		ON CONFLICT (authority, provider_id, external_id) DO UPDATE SET checkpoint_id = excluded.checkpoint_id, pinning = excluded.pinning`,
		modelID, ckID); err != nil {
		t.Fatalf("写调用行失败: %v", err)
	}

	// 复合外键的第二半：调用行的 `model_id` 与版本的 `model_id` 必须一致。
	// 单列外键下这个探针插得进去（实测过），库里于是出现「这一行说模型甲、版本却是乙的」。
	otherModel := mkCheckpointTestModel(t, pool, "it-ck-ref-b", "另一个模型")
	t.Cleanup(func() { cleanupCheckpointTest(t, pool, []string{"it-ck-ref-b"}) })
	otherCK, err := q.InsertModelCheckpoint(ctx, store.InsertModelCheckpointParams{
		ModelID: otherModel, Label: "1017", ReleasedOn: checkpointDate(2026, time.October, 17),
	})
	if err != nil {
		t.Fatalf("登记第二个模型的版本失败: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`UPDATE model_offerings SET checkpoint_id = $1 WHERE external_id = 'it-ck-ref-ext' AND model_id = $2`,
		otherCK, modelID); err == nil {
		t.Fatal("调用行挂别的模型的版本不该成功，实际改进去了")
	} else {
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23503" {
			t.Fatalf("跨模型挂版本应当撞外键（23503），实际: %v", err)
		}
		if pgErr.ConstraintName != "fk_model_offerings_checkpoint_id72" {
			t.Fatalf("撞的应当是 fk_model_offerings_checkpoint_id72，实际 %q", pgErr.ConstraintName)
		}
	}

	if _, err := pool.Exec(ctx, `DELETE FROM model_checkpoints WHERE id = $1`, ckID); err == nil {
		t.Fatal("被调用行引用的版本不该删得掉，实际删成功了")
	} else {
		// ON DELETE RESTRICT 在 PG 里报的是 23001（restrict_violation），不是 23503
		// （foreign_key_violation）——NO ACTION 才是后者。这条区分正是「先解除引用」
		// 与「等事务结束再判」的差别，用例按 RESTRICT 实测的码断言。
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23001" {
			t.Fatalf("删被引用版本应当报 RESTRICT 违规（23001），实际: %v", err)
		}
		if pgErr.ConstraintName != "fk_model_offerings_checkpoint_id72" {
			t.Fatalf("撞的应当是 fk_model_offerings_checkpoint_id72，实际 %q", pgErr.ConstraintName)
		}
	}

	// 有版本的模型也删不掉（models 一侧同为 RESTRICT）。
	if _, err := pool.Exec(ctx, `DELETE FROM models WHERE id = $1`, modelID); err == nil {
		t.Fatal("还有版本的模型不该删得掉，实际删成功了")
	}

	// 解除引用之后两样都能删：RESTRICT 是「先解除引用」，不是「永远删不掉」。
	if _, err := pool.Exec(ctx, `DELETE FROM model_offerings WHERE external_id = 'it-ck-ref-ext'`); err != nil {
		t.Fatalf("删调用行失败: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM model_checkpoints WHERE id = $1`, ckID); err != nil {
		t.Fatalf("解除引用后应当删得掉版本，实际: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM models WHERE id = $1`, modelID); err != nil {
		t.Fatalf("解除引用后应当删得掉模型，实际: %v", err)
	}
}

// TestIntegrationOfferingPinningDefaultsToUnknown 钉住 pinning 的默认值与取值范围。
//
// 为什么重要：默认值只能是「不知道」。默认成 exact 等于替端点编了一个它没说过的事实，
// 而这三个取值的区别（钉死 / 滚动 / 不知道）正是第二步要认真的地方。
func TestIntegrationOfferingPinningDefaultsToUnknown(t *testing.T) {
	pool := checkpointTestPool(t)
	ctx := context.Background()

	modelID := mkCheckpointTestModel(t, pool, "it-ck-pin", "版本 pinning 模型")
	t.Cleanup(func() { cleanupCheckpointTest(t, pool, []string{"it-ck-pin"}) })

	if _, err := pool.Exec(ctx, `
		INSERT INTO model_offerings (authority, provider_id, provider_name, external_id, model_id)
		VALUES ('endpoint', '', '', 'it-ck-pin-ext', $1)`, modelID); err != nil {
		t.Fatalf("写调用行失败: %v", err)
	}

	var pinning string
	var ckValid bool
	if err := pool.QueryRow(ctx,
		`SELECT pinning, checkpoint_id IS NOT NULL FROM model_offerings WHERE external_id = 'it-ck-pin-ext'`).
		Scan(&pinning, &ckValid); err != nil {
		t.Fatalf("读 pinning 失败: %v", err)
	}
	if pinning != "unknown" {
		t.Fatalf("没指定 pinning 时应当是 unknown，实际 %q", pinning)
	}
	if ckValid {
		t.Fatal("没指定版本时 checkpoint_id 应当是 NULL，实际有值")
	}

	if _, err := pool.Exec(ctx, `UPDATE model_offerings SET pinning = 'rolling' WHERE external_id = 'it-ck-pin-ext'`); err != nil {
		t.Fatalf("改成 rolling 应当允许: %v", err)
	}

	if _, err := pool.Exec(ctx, `UPDATE model_offerings SET pinning = 'pinned' WHERE external_id = 'it-ck-pin-ext'`); err == nil {
		t.Fatal("取值不在 exact / rolling / unknown 里应当被拒，实际写进去了")
	} else {
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
			t.Fatalf("非法 pinning 应当撞 CHECK 约束（23514），实际: %v", err)
		}
		if pgErr.ConstraintName != "ck_model_offerings_pinning" {
			t.Fatalf("撞的应当是 ck_model_offerings_pinning，实际 %q", pgErr.ConstraintName)
		}
	}
}

// TestIntegrationCheckpointIsWiredIntoRevisionModels 钉住作品侧那条引用真的能用。
//
// 4b（作品页显示「用的是哪一版」）要往 demo_revision_models.checkpoint_id 里写值。
// 这一列与它的外键是本笔加的，若只在 model_offerings 那一侧验证过，作品侧漏建列要到 4b
// 才被发现——那时错误会出现在另一个 PR 的测试里，定位成本高得多。
//
// 首版只查 information_schema，review 实测指出那不够：把外键改成 CASCADE，五条用例全绿。
// 所以这里走真实写入——能挂上、挂错模型会被挡、挂上后版本删不掉、置回 NULL 后能删掉。
func TestIntegrationCheckpointIsWiredIntoRevisionModels(t *testing.T) {
	pool := checkpointTestPool(t)
	ctx := context.Background()
	q := store.New(pool)

	modelA := mkCheckpointTestModel(t, pool, "it-ck-rev-a", "作品版本模型甲")
	modelB := mkCheckpointTestModel(t, pool, "it-ck-rev-b", "作品版本模型乙")
	t.Cleanup(func() { cleanupCheckpointTest(t, pool, []string{"it-ck-rev-a", "it-ck-rev-b"}) })

	ckA, err := q.InsertModelCheckpoint(ctx, store.InsertModelCheckpointParams{
		ModelID: modelA, Label: "1017", ReleasedOn: checkpointDate(2026, time.October, 17),
	})
	if err != nil {
		t.Fatalf("登记甲模型的版本失败: %v", err)
	}
	ckB, err := q.InsertModelCheckpoint(ctx, store.InsertModelCheckpointParams{
		ModelID: modelB, Label: "1017", ReleasedOn: checkpointDate(2026, time.October, 17),
	})
	if err != nil {
		t.Fatalf("登记乙模型的版本失败: %v", err)
	}

	revID := mkCheckpointTestDemo(t, pool, "it-ck-rev-demo")
	t.Cleanup(func() { cleanupCheckpointDemo(t, pool, "it-ck-rev-demo") })

	// 1) 挂上本模型的版本：作品侧这条路真的通。
	if _, err := pool.Exec(ctx, `
		INSERT INTO demo_revision_models (revision_id, model_id, checkpoint_id, source)
		VALUES ($1, $2, $3, 'api')`, revID, modelA, ckA); err != nil {
		t.Fatalf("给作品版本挂本模型的 checkpoint 失败: %v", err)
	}

	// 2) 挂别的模型的版本：复合外键挡住（单列外键放行过，见文件头）。
	if _, err := pool.Exec(ctx, `
		UPDATE demo_revision_models SET checkpoint_id = $1 WHERE revision_id = $2 AND model_id = $3`,
		ckB, revID, modelA); err == nil {
		t.Fatal("作品版本挂别的模型的 checkpoint 不该成功，实际改进去了")
	} else {
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23503" {
			t.Fatalf("跨模型挂版本应当撞外键（23503），实际: %v", err)
		}
		if pgErr.ConstraintName != "fk_demo_revision_models_checkpoint_id71" {
			t.Fatalf("撞的应当是 fk_demo_revision_models_checkpoint_id71，实际 %q", pgErr.ConstraintName)
		}
	}

	// 3) 被作品版本引用的 checkpoint 删不掉：RESTRICT，不是 CASCADE。
	if _, err := pool.Exec(ctx, `DELETE FROM model_checkpoints WHERE id = $1`, ckA); err == nil {
		t.Fatal("被作品版本引用的 checkpoint 不该删得掉，实际删成功了")
	} else {
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23001" {
			t.Fatalf("删被作品引用的版本应当报 RESTRICT 违规（23001），实际: %v", err)
		}
		if pgErr.ConstraintName != "fk_demo_revision_models_checkpoint_id71" {
			t.Fatalf("撞的应当是 fk_demo_revision_models_checkpoint_id71，实际 %q", pgErr.ConstraintName)
		}
	}

	// 4) 置回 NULL 是「未指定」：之后版本就删得掉，模型声明本身还在。
	if _, err := pool.Exec(ctx, `
		UPDATE demo_revision_models SET checkpoint_id = NULL WHERE revision_id = $1 AND model_id = $2`,
		revID, modelA); err != nil {
		t.Fatalf("把 checkpoint 置回 NULL 失败: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM model_checkpoints WHERE id = $1`, ckA); err != nil {
		t.Fatalf("解除引用后应当删得掉版本，实际: %v", err)
	}
	var stillThere int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM demo_revision_models WHERE revision_id = $1 AND model_id = $2`,
		revID, modelA).Scan(&stillThere); err != nil {
		t.Fatalf("回查模型声明失败: %v", err)
	}
	if stillThere != 1 {
		t.Fatalf("置 NULL 不该删掉模型声明本身，实际剩 %d 行", stillThere)
	}
}
