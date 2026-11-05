package demo

import (
	"context"
	"errors"
	"os"
	"testing"
)

// 版本历史与回滚的集成测试。
//
// 这一组要钉住的核心不是「能不能回滚」，而是**回滚之后对外还是原来那一版**：
// 回滚 = 追加一个待审的新版本，不是让线上内容立刻退回旧版。

// TestIntegrationListRevisions 版本列表按版本号倒序，两个指针标对。
func TestIntegrationListRevisions(t *testing.T) {
	pool := testPool(t)
	s := testService(t, pool)
	author := mkAuthor(t, pool, "rev-list")
	slug := submitForUpdate(t, pool, s, author, "版本列表")
	approveAndPublish(t, pool, slug)

	ctx := context.Background()
	if _, err := s.Update(ctx, UpdateInput{
		AuthorID: author, Slug: slug, Title: "第二版标题",
	}); err != nil {
		t.Fatalf("更新失败: %v", err)
	}

	revisions, err := s.ListRevisions(ctx, slug)
	if err != nil {
		t.Fatalf("列版本失败: %v", err)
	}
	if len(revisions) != 2 {
		t.Fatalf("期望 2 个版本，实际 %d", len(revisions))
	}
	// 新的在前。
	if revisions[0].RevNo != 2 || revisions[1].RevNo != 1 {
		t.Fatalf("顺序不对：%d, %d", revisions[0].RevNo, revisions[1].RevNo)
	}
	// 复审期间：当前版本是 r2，对外版本是 r1——这就是「改稿不等于下线」。
	if !revisions[0].IsCurrent || revisions[0].IsPublished {
		t.Fatalf("r2 应该是当前版本且不是对外版本，实际 %+v", revisions[0])
	}
	if revisions[1].IsCurrent || !revisions[1].IsPublished {
		t.Fatalf("r1 应该是对外版本且不是当前版本，实际 %+v", revisions[1])
	}
	if revisions[0].Title != "第二版标题" {
		t.Fatalf("r2 标题 = %q", revisions[0].Title)
	}
}

// TestIntegrationRestoreAppendsRevision 回滚追加新版本、清单指向同一批 blob、
// **对外版本不动**、审核态回到 pending。
func TestIntegrationRestoreAppendsRevision(t *testing.T) {
	pool := testPool(t)
	s := testService(t, pool)
	author := mkAuthor(t, pool, "rev-restore")
	slug := submitForUpdate(t, pool, s, author, "回滚测试·第一版")
	approveAndPublish(t, pool, slug)

	ctx := context.Background()
	publishedBefore := publishedRevisionID(t, pool, slug)
	manifestV1 := manifestOf(t, pool, currentRevisionID(t, pool, slug))

	// 换文件改成 r2（内容变了，blob 也变了）。
	zipPath := buildZip(t, map[string]string{"index.html": "<!doctype html><p>v2</p>"})
	info, statErr := os.Stat(zipPath)
	if statErr != nil {
		t.Fatalf("读包大小失败: %v", statErr)
	}
	if _, err := s.Update(ctx, UpdateInput{
		AuthorID:    author,
		Slug:        slug,
		Title:       "回滚测试·第二版",
		DemoType:    "web",
		PackagePath: zipPath,
		PackageName: "demo.zip",
		PackageSize: info.Size(),
	}); err != nil {
		t.Fatalf("更新失败: %v", err)
	}

	// 回滚到 r1。
	res, err := s.Restore(ctx, slug, 1, author)
	if err != nil {
		t.Fatalf("回滚失败: %v", err)
	}
	if res.RestoredFromNo != 1 {
		t.Fatalf("来源版本 = %d，想要 1", res.RestoredFromNo)
	}
	if res.NewRevNo != 3 {
		t.Fatalf("新版本号 = %d，想要 3", res.NewRevNo)
	}
	if res.ModerationState != "pending" {
		t.Fatalf("回滚后应进入待审，实际 %s", res.ModerationState)
	}

	// 对外版本必须还是原来那一版——回滚不等于「线上立刻变回旧版」。
	if after := publishedRevisionID(t, pool, slug); after != publishedBefore {
		t.Fatalf("对外版本被动了：%d → %d", publishedBefore, after)
	}

	// 新版本的清单必须与 r1 一致（同一批 blob，零字节写入）。
	manifestV3 := manifestOf(t, pool, currentRevisionID(t, pool, slug))
	if len(manifestV3) != len(manifestV1) {
		t.Fatalf("清单条目数 = %d，想要 %d", len(manifestV3), len(manifestV1))
	}
	for path, blob := range manifestV1 {
		if manifestV3[path] != blob {
			t.Fatalf("%s 的 blob 变了：%s → %s", path, blob, manifestV3[path])
		}
	}

	// 时间线里必须能看出「谁在什么时候回滚了」。
	var kind, message string
	if err := pool.QueryRow(ctx, `
		SELECT kind, COALESCE(message, '') FROM demo_timeline
		WHERE demo_id = (SELECT id FROM demos WHERE slug = $1) AND kind = 'rollback'
		ORDER BY id DESC LIMIT 1`, slug).Scan(&kind, &message); err != nil {
		t.Fatalf("没找到回滚时间线: %v", err)
	}
	if message == "" {
		t.Fatalf("回滚时间线没有说明文字")
	}
}

// TestIntegrationRestoreKeepsCurrentIsRejected 回滚到「当前版本」：400。
//
// 不拒绝的话会白造一个内容完全一样的版本，作者还得再等一次审核。
func TestIntegrationRestoreKeepsCurrentIsRejected(t *testing.T) {
	pool := testPool(t)
	s := testService(t, pool)
	author := mkAuthor(t, pool, "rev-same")
	slug := submitForUpdate(t, pool, s, author, "回滚到当前")

	_, err := s.Restore(context.Background(), slug, 1, author)
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("期望 ErrInvalidInput，实际 %v", err)
	}
}

// TestIntegrationRestoreRejectsOthers 别人的作品：404 口径（ErrNotFound）。
func TestIntegrationRestoreRejectsOthers(t *testing.T) {
	pool := testPool(t)
	s := testService(t, pool)
	author := mkAuthor(t, pool, "rev-owner")
	other := mkAuthor(t, pool, "rev-other")
	slug := submitForUpdate(t, pool, s, author, "越权回滚")

	_, err := s.Restore(context.Background(), slug, 1, other)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("期望 ErrNotFound，实际 %v", err)
	}
}

// TestIntegrationRestoreMissingRevision 不存在的版本号：ErrRevisionNotFound。
func TestIntegrationRestoreMissingRevision(t *testing.T) {
	pool := testPool(t)
	s := testService(t, pool)
	author := mkAuthor(t, pool, "rev-missing")
	slug := submitForUpdate(t, pool, s, author, "不存在的版本")

	_, err := s.Restore(context.Background(), slug, 9, author)
	if !errors.Is(err, ErrRevisionNotFound) {
		t.Fatalf("期望 ErrRevisionNotFound，实际 %v", err)
	}
}

// TestIntegrationRestoreRejectsErasedRevision 目标版本被擦除：回滚过去等于产出空版本。
func TestIntegrationRestoreRejectsErasedRevision(t *testing.T) {
	pool := testPool(t)
	s := testService(t, pool)
	author := mkAuthor(t, pool, "rev-erased")
	slug := submitForUpdate(t, pool, s, author, "擦除的版本")

	ctx := context.Background()
	if _, err := s.Update(ctx, UpdateInput{
		AuthorID: author, Slug: slug, Title: "第二版",
	}); err != nil {
		t.Fatalf("更新失败: %v", err)
	}
	// 把 r1 擦掉。ck_rev_erased_content 要求**所有内容列**都为空——
	// 只清 title/description 会撞约束，这本身就是「擦除」与「软删」的区别：
	// 软删只藏起来，擦除是内容真的不在了。
	// ck_rev_time_order 还要求 erased_at 之前必须已有 deleted_at——先删后擦，顺序不能倒。
	if _, err := pool.Exec(ctx, `
		UPDATE demo_revisions
		SET title = NULL, description = NULL, cover_url = NULL, cover_inline = NULL,
		    entry_path = NULL, manifest_hash = NULL, file_count = NULL, total_bytes = NULL,
		    deleted_at = now(), erased_at = now()
		WHERE demo_id = (SELECT id FROM demos WHERE slug = $1) AND rev_no = 1`, slug); err != nil {
		t.Fatalf("擦除 r1 失败: %v", err)
	}

	_, err := s.Restore(ctx, slug, 1, author)
	if !errors.Is(err, ErrRevisionNotFound) {
		t.Fatalf("期望 ErrRevisionNotFound，实际 %v", err)
	}
}
