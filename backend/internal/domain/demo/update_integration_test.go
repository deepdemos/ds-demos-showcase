package demo

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// 更新的集成测试：连真库 + 真文件系统。
//
// 这一组的核心不是「能不能改」，而是「改了之后**对外那一版还在不在**」——
// 迁移 0014 把当前版本与对外版本拆开，更新只该动前者。

// submitForUpdate 投一件作品，返回 slug 与第一个版本号。
//
// 会先清掉「没人引用」的 blobs 行。不清的话，上一轮测试留下的 blobs 行会让
// storeBlob 认为「库里已有、不必写盘」，而磁盘是这一轮的 t.TempDir——
// 于是清单指向一个不存在的对象。这正是「去重信任数据库」那条取舍的反面：
// 数据库说有的东西，存储里必须有；换了存储后端，所有去重命中都会变成空指针。
func submitForUpdate(t *testing.T, pool *pgxpool.Pool, s *Service, authorID int64, title string) string {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`DELETE FROM blobs WHERE sha256 NOT IN (SELECT DISTINCT blob_hash FROM demo_revision_files)`); err != nil {
		t.Fatalf("清理孤儿 blob 行失败: %v", err)
	}
	zipPath := buildZip(t, map[string]string{
		"index.html": "<!doctype html><html><body>v1</body></html>",
		"app.js":     "console.log('v1')",
	})
	info, err := os.Stat(zipPath)
	if err != nil {
		t.Fatalf("读包大小失败: %v", err)
	}
	res, err := s.Submit(context.Background(), SubmitInput{
		AuthorID:    authorID,
		Title:       title,
		Description: "第一版简介",
		DemoType:    "web",
		PackagePath: zipPath,
		PackageName: "demo.zip",
		PackageSize: info.Size(),
	})
	if err != nil {
		t.Fatalf("投稿失败: %v", err)
	}
	return res.Slug
}

// approveAndPublish 让作品对外可见：审核通过 + 发布。
//
// 直接改库而不是走 Review/Publish 服务方法：这一组验的是「更新」，把前面几步
// 的失败面也拖进来，一个红点要查三层。
func approveAndPublish(t *testing.T, pool *pgxpool.Pool, slug string) {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(ctx,
		`UPDATE demos SET moderation_state = 'approved', published_revision_id = current_revision_id,
		 lifecycle_state = 'published' WHERE slug = $1`, slug); err != nil {
		t.Fatalf("置为已发布失败: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO demo_channels (demo_id, channel, visibility, lang)
		 SELECT id, 'deep', 'listed', 'zh' FROM demos WHERE slug = $1
		 ON CONFLICT DO NOTHING`, slug); err != nil {
		t.Fatalf("建渠道失败: %v", err)
	}
}

// TestIntegrationUpdateAppendsRevisionTextOnly 只改文字：新版本号 +1，
// 清单整表复制（同一批 blob），审核态回 pending，**对外版本不动**。
func TestIntegrationUpdateAppendsRevisionTextOnly(t *testing.T) {
	pool := testPool(t)
	s := testService(t, pool)
	author := mkAuthor(t, pool, "update-text")
	slug := submitForUpdate(t, pool, s, author, "更新测试·只改文字")
	approveAndPublish(t, pool, slug)

	ctx := context.Background()
	var beforeRevID int64
	var beforePublishedRevNo int
	if err := pool.QueryRow(ctx, `
		SELECT d.published_revision_id, r.rev_no
		FROM demos d JOIN demo_revisions r ON r.id = d.published_revision_id
		WHERE d.slug = $1`, slug).Scan(&beforeRevID, &beforePublishedRevNo); err != nil {
		t.Fatalf("读初始状态失败: %v", err)
	}

	res, err := s.Update(ctx, UpdateInput{
		AuthorID:    author,
		Slug:        slug,
		Title:       "更新测试·新标题",
		Description: "换了一个简介",
	})
	if err != nil {
		t.Fatalf("更新失败: %v", err)
	}
	if res.RevisionNo != 2 {
		t.Fatalf("期望版本号 2，实际 %d", res.RevisionNo)
	}
	if res.FilesChanged {
		t.Fatalf("没传包不该标记为换了文件")
	}
	if res.ModerationState != "pending" {
		t.Fatalf("期望回到 pending，实际 %s", res.ModerationState)
	}

	// 当前版本前进了，对外版本没动。
	var curRevNo, pubRevID int64
	var publishedRevNo int
	var moderation string
	if err := pool.QueryRow(ctx, `
		SELECT cr.rev_no, d.published_revision_id, pr.rev_no, d.moderation_state
		FROM demos d
		JOIN demo_revisions cr ON cr.id = d.current_revision_id
		JOIN demo_revisions pr ON pr.id = d.published_revision_id
		WHERE d.slug = $1`, slug).Scan(&curRevNo, &pubRevID, &publishedRevNo, &moderation); err != nil {
		t.Fatalf("读更新后状态失败: %v", err)
	}
	if curRevNo != 2 {
		t.Fatalf("当前版本期望 2，实际 %d", curRevNo)
	}
	if pubRevID != beforeRevID || publishedRevNo != beforePublishedRevNo {
		t.Fatalf("对外版本被改动了：期望仍是版本 %d（id=%d），实际 %d（id=%d）",
			beforePublishedRevNo, beforeRevID, publishedRevNo, pubRevID)
	}
	if moderation != "pending" {
		t.Fatalf("审核态期望 pending，实际 %s", moderation)
	}

	// 清单被整表复制：路径集合一致，且指向**同一批 blob**（零拷贝）。
	oldFiles := manifestOf(t, pool, beforeRevID)
	newFiles := manifestOf(t, pool, currentRevisionID(t, pool, slug))
	if len(newFiles) != len(oldFiles) {
		t.Fatalf("清单行数变了：旧 %d 新 %d", len(oldFiles), len(newFiles))
	}
	for path, hash := range oldFiles {
		if newFiles[path] != hash {
			t.Fatalf("路径 %s 的 blob 变了：旧 %s 新 %s（只改文字不该写新 blob）", path, hash, newFiles[path])
		}
	}
}

// TestIntegrationUpdateKeepsOldVersionVisible 换文件：新版本有新的 blob，
// 但**对外那一版的内容仍然可读**——这是 C 方案（旧版本继续服务）的核心断言。
func TestIntegrationUpdateKeepsOldVersionVisible(t *testing.T) {
	pool := testPool(t)
	s := testService(t, pool)
	author := mkAuthor(t, pool, "update-file")
	slug := submitForUpdate(t, pool, s, author, "更新测试·换文件")
	approveAndPublish(t, pool, slug)

	ctx := context.Background()
	pubRevID := publishedRevisionID(t, pool, slug)

	zipPath := buildZip(t, map[string]string{
		"index.html": "<!doctype html><html><body>v2</body></html>",
		"app.js":     "console.log('v2')",
	})
	info, statErr := os.Stat(zipPath)
	if statErr != nil {
		t.Fatalf("读包大小失败: %v", statErr)
	}
	res, err := s.Update(ctx, UpdateInput{
		AuthorID:    author,
		Slug:        slug,
		Title:       "更新测试·换文件",
		Description: "第二版",
		DemoType:    "web",
		PackagePath: zipPath,
		PackageName: "demo.zip",
		PackageSize: info.Size(),
	})
	if err != nil {
		t.Fatalf("更新失败: %v", err)
	}
	if !res.FilesChanged {
		t.Fatalf("传了包应当标记为换了文件")
	}
	if res.RevisionNo != 2 {
		t.Fatalf("期望版本号 2，实际 %d", res.RevisionNo)
	}

	// 对外版本还是 r1：它的清单指向**旧**的 blob。
	if got := publishedRevisionID(t, pool, slug); got != pubRevID {
		t.Fatalf("对外版本被换掉了：旧 %d 新 %d", pubRevID, got)
	}
	oldFiles := manifestOf(t, pool, pubRevID)
	newFiles := manifestOf(t, pool, currentRevisionID(t, pool, slug))
	if oldFiles["index.html"] == newFiles["index.html"] {
		t.Fatalf("内容换了，两个版本的 index.html 不该指向同一份 blob")
	}

	// 旧版本的字节仍然读得到：对外内容不能因为「有人点了保存」就消失。
	var key string
	if err := pool.QueryRow(ctx,
		`SELECT storage_key FROM blobs WHERE sha256 = $1`, oldFiles["index.html"]).Scan(&key); err != nil {
		t.Fatalf("读旧 blob 失败: %v", err)
	}
	f, err := s.files.Get(ctx, key)
	if err != nil {
		t.Fatalf("旧版本的字节读不出来了: %v", err)
	}
	defer func() { _ = f.Close() }()
}

// TestIntegrationUpdateRejectsOthers 不是作者一律 ErrNotFound（403 会泄露 slug 存在）。
func TestIntegrationUpdateRejectsOthers(t *testing.T) {
	pool := testPool(t)
	s := testService(t, pool)
	author := mkAuthor(t, pool, "update-owner")
	other := mkAuthor(t, pool, "update-other")
	slug := submitForUpdate(t, pool, s, author, "更新测试·越权")

	_, err := s.Update(context.Background(), UpdateInput{
		AuthorID: other,
		Slug:     slug,
		Title:    "抢改别人的作品",
	})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("期望 ErrNotFound，实际 %v", err)
	}
}

// TestIntegrationUpdateRejectsTypeChangeWithoutPackage 只改类型不给包：400。
//
// 类型是内容决定的。允许「文字说 zip、文件是 html」会让下载与预览指着不同的事实。
func TestIntegrationUpdateRejectsTypeChangeWithoutPackage(t *testing.T) {
	pool := testPool(t)
	s := testService(t, pool)
	author := mkAuthor(t, pool, "update-type")
	slug := submitForUpdate(t, pool, s, author, "更新测试·换类型")

	_, err := s.Update(context.Background(), UpdateInput{
		AuthorID: author,
		Slug:     slug,
		Title:    "更新测试·换类型",
		DemoType: "zip",
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("期望 ErrInvalidInput，实际 %v", err)
	}
}

// TestIntegrationUpdateRejectsRemoved 被平台移除的不允许作者自行改回。
func TestIntegrationUpdateRejectsRemoved(t *testing.T) {
	pool := testPool(t)
	s := testService(t, pool)
	author := mkAuthor(t, pool, "update-removed")
	slug := submitForUpdate(t, pool, s, author, "更新测试·已移除")

	ctx := context.Background()
	// 移除要求先把渠道藏起来（trg_demos_guard 不允许还有 listed 渠道的作品离开 published）。
	if _, err := pool.Exec(ctx,
		`UPDATE demos SET lifecycle_state = 'removed', moderation_state = 'approved' WHERE slug = $1`,
		slug); err != nil {
		t.Fatalf("置为 removed 失败: %v", err)
	}

	_, err := s.Update(ctx, UpdateInput{AuthorID: author, Slug: slug, Title: "改回来"})
	if !errors.Is(err, ErrNotEditable) {
		t.Fatalf("期望 ErrNotEditable，实际 %v", err)
	}
}

// ---- 辅助 ----

func currentRevisionID(t *testing.T, pool *pgxpool.Pool, slug string) int64 {
	t.Helper()
	var id int64
	if err := pool.QueryRow(context.Background(),
		`SELECT current_revision_id FROM demos WHERE slug = $1`, slug).Scan(&id); err != nil {
		t.Fatalf("读当前版本失败: %v", err)
	}
	return id
}

func publishedRevisionID(t *testing.T, pool *pgxpool.Pool, slug string) int64 {
	t.Helper()
	var id int64
	if err := pool.QueryRow(context.Background(),
		`SELECT published_revision_id FROM demos WHERE slug = $1`, slug).Scan(&id); err != nil {
		t.Fatalf("读对外版本失败: %v", err)
	}
	return id
}

func manifestOf(t *testing.T, pool *pgxpool.Pool, revID int64) map[string]string {
	t.Helper()
	rows, err := pool.Query(context.Background(),
		`SELECT path, blob_hash FROM demo_revision_files WHERE revision_id = $1`, revID)
	if err != nil {
		t.Fatalf("读清单失败: %v", err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var p, h string
		if err := rows.Scan(&p, &h); err != nil {
			t.Fatalf("读清单行失败: %v", err)
		}
		out[p] = h
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("读清单失败: %v", err)
	}
	return out
}

var _ = store.New
