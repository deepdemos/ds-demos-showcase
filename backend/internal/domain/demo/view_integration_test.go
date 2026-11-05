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
