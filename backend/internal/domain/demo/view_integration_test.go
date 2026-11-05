package demo

import (
	"context"
	"testing"
)

// 浏览计数的核心承诺是「刷新不刷高」，所以这里逐条钉住去重与 +1 的关系。
//
// 注意这个包里的 `submitForUpdate` 会先清孤儿 blob 行，这里不用它——
// 视图测试不读字节，只读计数，直接复用投稿入口即可。

// viewCountOf 读某作品的浏览数。行不存在返回 0（demo_stats 是懒建的）。
func viewCountOf(t *testing.T, s *Service, slug string) int64 {
	t.Helper()
	var n int64
	err := s.pool.QueryRow(context.Background(),
		`SELECT COALESCE(st.view_count, 0) FROM demos d
		 LEFT JOIN demo_stats st ON st.demo_id = d.id WHERE d.slug = $1`, slug).Scan(&n)
	if err != nil {
		t.Fatalf("读浏览数失败: %v", err)
	}
	return n
}

// TestIntegrationRecordViewDedupesPerDay 同一个人同一天看两次，只算一次。
func TestIntegrationRecordViewDedupesPerDay(t *testing.T) {
	pool := testPool(t)
	s := testService(t, pool)
	author := mkAuthor(t, pool, "view-dedupe")
	slug := submitZipForView(t, s, author, "浏览去重")
	approveAndPublish(t, pool, slug)

	first, err := s.RecordView(context.Background(), slug, "user:1001")
	if err != nil {
		t.Fatalf("第一次记浏览失败: %v", err)
	}
	if !first {
		t.Fatalf("第一次浏览应当计入")
	}

	second, err := s.RecordView(context.Background(), slug, "user:1001")
	if err != nil {
		t.Fatalf("第二次记浏览失败: %v", err)
	}
	if second {
		t.Fatalf("同一天同一人第二次浏览不该再计入")
	}

	if got := viewCountOf(t, s, slug); got != 1 {
		t.Fatalf("浏览数应为 1，实际 %d", got)
	}
}

// TestIntegrationRecordViewCountsDifferentViewers 不同的人各算一次。
func TestIntegrationRecordViewCountsDifferentViewers(t *testing.T) {
	pool := testPool(t)
	s := testService(t, pool)
	author := mkAuthor(t, pool, "view-multi")
	slug := submitZipForView(t, s, author, "多人浏览")
	approveAndPublish(t, pool, slug)

	for _, key := range []string{"user:1", "user:2", "anon:abc"} {
		if ok, err := s.RecordView(context.Background(), slug, key); err != nil || !ok {
			t.Fatalf("%s 的浏览应当计入（ok=%v err=%v）", key, ok, err)
		}
	}
	if got := viewCountOf(t, s, slug); got != 3 {
		t.Fatalf("浏览数应为 3，实际 %d", got)
	}
}

// TestIntegrationRecordViewSkipsUnpublished 未对外可见的作品不计浏览。
//
// 这一条是计数的第一道闸：草稿、待审、已下架、已软删都不该有浏览量。
// 少了它，作者自己反复打开草稿就能把数字刷上去，而那正是运营最先会看的地方。
func TestIntegrationRecordViewSkipsUnpublished(t *testing.T) {
	pool := testPool(t)
	s := testService(t, pool)
	author := mkAuthor(t, pool, "view-unpublished")
	slug := submitZipForView(t, s, author, "未发布不计浏览")

	if ok, err := s.RecordView(context.Background(), slug, "user:1"); err != nil || ok {
		t.Fatalf("草稿不该计浏览（ok=%v err=%v）", ok, err)
	}

	approveAndPublish(t, pool, slug)
	if ok, err := s.RecordView(context.Background(), slug, "user:1"); err != nil || !ok {
		t.Fatalf("发布后该计浏览（ok=%v err=%v）", ok, err)
	}

	// 下架之后不再计：对外已经是墓碑页，不该继续涨。
	if _, err := pool.Exec(context.Background(),
		`UPDATE demos SET lifecycle_state = 'archived' WHERE slug = $1`, slug); err != nil {
		t.Fatalf("下架失败: %v", err)
	}
	if ok, err := s.RecordView(context.Background(), slug, "user:2"); err != nil || ok {
		t.Fatalf("已下架不该计浏览（ok=%v err=%v）", ok, err)
	}
}

// TestIntegrationRecordViewIsAtomic 计数失败时不该留下去重行。
//
// 这是 F3 那条缺陷的可证伪形态：如果去重与 +1 是两条语句，第一步先落库、
// 第二步失败，那么这个人**当天再也加不上了**——去重行在，以后每次都是冲突。
// 表现是「浏览数永远差一次」，而且没有错误、没有日志，只有一次数据库抖动。
//
// 造法：给 demo_stats 挂一个必然失败的 BEFORE INSERT 触发器，让 +1 那半条路失败。
// 断言的不是「函数返回错误」（那太容易满足），而是**去重行没有留下**：
// 触发器拆掉之后同一个人再记一次，必须还能记上。
func TestIntegrationRecordViewIsAtomic(t *testing.T) {
	pool := testPool(t)
	s := testService(t, pool)
	author := mkAuthor(t, pool, "view-atomic")
	slug := submitZipForView(t, s, author, "浏览原子性")
	approveAndPublish(t, pool, slug)

	ctx := context.Background()
	if _, err := pool.Exec(ctx, `
		CREATE OR REPLACE FUNCTION it_fail_stats() RETURNS trigger AS $$
		BEGIN RAISE EXCEPTION 'it: demo_stats 写不进去'; END $$ LANGUAGE plpgsql`); err != nil {
		t.Fatalf("建触发器函数失败: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`CREATE TRIGGER it_fail_stats BEFORE INSERT ON demo_stats
		 FOR EACH ROW EXECUTE FUNCTION it_fail_stats()`); err != nil {
		t.Fatalf("建触发器失败: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DROP TRIGGER IF EXISTS it_fail_stats ON demo_stats`)
		_, _ = pool.Exec(context.Background(), `DROP FUNCTION IF EXISTS it_fail_stats()`)
	})

	if _, err := s.RecordView(ctx, slug, "user:7001"); err == nil {
		t.Fatalf("计数写不进去时应当报错，实际成功了")
	}

	var views int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM demo_views v JOIN demos d ON d.id = v.demo_id WHERE d.slug = $1`,
		slug).Scan(&views); err != nil {
		t.Fatalf("查去重行失败: %v", err)
	}
	if views != 0 {
		t.Fatalf("计数失败却留下了 %d 行去重行——这个人今天再也加不上了", views)
	}

	// 拆掉触发器，同一个人再记一次：必须计入，且浏览数正好是 1。
	if _, err := pool.Exec(ctx, `DROP TRIGGER IF EXISTS it_fail_stats ON demo_stats`); err != nil {
		t.Fatalf("拆触发器失败: %v", err)
	}
	counted, err := s.RecordView(ctx, slug, "user:7001")
	if err != nil {
		t.Fatalf("拆掉触发器后记浏览失败: %v", err)
	}
	if !counted {
		t.Fatalf("失败的那次不该占用今天的名额，这一次应当计入")
	}
	if got := viewCountOf(t, s, slug); got != 1 {
		t.Fatalf("浏览数 = %d，想要 1", got)
	}
}

// TestIntegrationRecordViewIgnoresEmptyKey 没有标识就不计，也不报错。
//
// 空键如果放进去，所有人会被并成同一个人——那比不计数更糟。
func TestIntegrationRecordViewIgnoresEmptyKey(t *testing.T) {
	pool := testPool(t)
	s := testService(t, pool)
	author := mkAuthor(t, pool, "view-empty")
	slug := submitZipForView(t, s, author, "空标识")
	approveAndPublish(t, pool, slug)

	if ok, err := s.RecordView(context.Background(), slug, ""); err != nil || ok {
		t.Fatalf("空标识不该计浏览（ok=%v err=%v）", ok, err)
	}
	if got := viewCountOf(t, s, slug); got != 0 {
		t.Fatalf("浏览数应为 0，实际 %d", got)
	}
}

// submitZipForView 走投稿入口造一件作品，返回 slug。
func submitZipForView(t *testing.T, s *Service, authorID int64, title string) string {
	t.Helper()
	zipPath := buildZip(t, map[string]string{
		"index.html": "<!doctype html><p>view</p>",
	})
	res, err := s.Submit(context.Background(), SubmitInput{
		AuthorID:    authorID,
		Title:       title,
		Description: "简介",
		DemoType:    "web",
		PackagePath: zipPath,
		PackageName: "demo.zip",
		PackageSize: fileSize(t, zipPath),
	})
	if err != nil {
		t.Fatalf("投稿失败: %v", err)
	}
	return res.Slug
}
