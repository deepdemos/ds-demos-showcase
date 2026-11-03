package demo

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/platform/storage"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// 作品域的集成测试：连真库 + 真文件系统跑整条投稿链路。
// 默认跳过（TEST_DATABASE_URL 未设置时）。
func testPool(t *testing.T) *pgxpool.Pool {
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

func testService(t *testing.T, pool *pgxpool.Pool) *Service {
	t.Helper()
	root := t.TempDir()
	files, err := storage.NewLocal(root)
	if err != nil {
		t.Fatalf("初始化存储失败: %v", err)
	}
	return New(pool, store.New(pool), files, storage.DefaultLimits(), 200<<20, nil)
}

// mkAuthor 建一个作者：accounts 行 + 对应的 users 投影行，返回 users.id。
//
// 不走 Register：投稿只需要一个 author_id，而注册会连带建 profiles / credentials /
// sessions，跟本域要验的东西无关。这里只满足 accounts 的约束。
func mkAuthor(t *testing.T, pool *pgxpool.Pool, tag string) int64 {
	t.Helper()
	sub := fmt.Sprintf("it-demo-%s", tag)
	var id int64
	err := pool.QueryRow(context.Background(),
		`INSERT INTO accounts (sub, status, role, level)
		 VALUES ($1, 'active', 'user', 0) RETURNING id`, sub,
	).Scan(&id)
	if err != nil {
		t.Fatalf("建作者失败: %v", err)
	}
	// demos.author_id 指向 users（demo 站投影表），不是 accounts。
	var userID int64
	if err := pool.QueryRow(context.Background(),
		`INSERT INTO users (sub) VALUES ($1) RETURNING id`, sub,
	).Scan(&userID); err != nil {
		t.Fatalf("建用户投影失败: %v", err)
	}
	t.Cleanup(func() {
		// 版本行由 fk_demo_revisions_demo_id76 的 ON DELETE CASCADE 带走（迁移 0006）。
		_, _ = pool.Exec(context.Background(), `DELETE FROM demos WHERE author_id = $1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM accounts WHERE id = $1`, id)
	})
	return userID
}

// buildZip 造一个最小可用的作品包。
func buildZip(t *testing.T, files map[string]string) string {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("造包失败: %v", err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatalf("造包失败: %v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("造包失败: %v", err)
	}
	path := filepath.Join(t.TempDir(), "demo.zip")
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatalf("写包失败: %v", err)
	}
	return path
}

func writeFile(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("写文件失败: %v", err)
	}
	return path
}

func TestIntegrationSubmitZip(t *testing.T) {
	pool := testPool(t)
	s := testService(t, pool)
	author := mkAuthor(t, pool, "zip")

	pkg := buildZip(t, map[string]string{
		"index.html":    "<html>hi</html>",
		"assets/app.js": "console.log(1)",
	})

	res, err := s.Submit(context.Background(), SubmitInput{
		AuthorID:    author,
		Title:       "布料物理",
		Description: "一次投稿",
		DemoType:    "web",
		PackagePath: pkg,
		PackageSize: fileSize(t, pkg),
	})
	if err != nil {
		t.Fatalf("投稿失败: %v", err)
	}
	if res.Slug == "" {
		t.Fatal("slug 不该为空")
	}
	if res.RevisionNo != 1 {
		t.Fatalf("rev_no = %d, want 1", res.RevisionNo)
	}
	if res.ModerationState != "pending" || res.LifecycleState != "draft" {
		t.Fatalf("状态应为 pending/draft，实际 %s/%s", res.ModerationState, res.LifecycleState)
	}

	// 文件真的落盘了：入口 + 资源 + 原始包。
	for _, key := range []string{
		storage.FilesKey(res.Slug, 1, "index.html"),
		storage.FilesKey(res.Slug, 1, "assets/app.js"),
		storage.SourceKey(res.Slug, 1, "zip"),
	} {
		if _, err := s.files.Stat(context.Background(), key); err != nil {
			t.Fatalf("文件 %s 未落盘: %v", key, err)
		}
	}

	// 库里这一行对外不可见：列表查询查不到。
	var listed int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM demos WHERE slug = $1 AND lifecycle_state = 'published'`, res.Slug,
	).Scan(&listed); err != nil {
		t.Fatalf("查列表失败: %v", err)
	}
	if listed != 0 {
		t.Fatal("刚投稿的作品不该出现在已发布列表里")
	}
}

// 单文件作品：不额外存一份原始包（它本身就是全部内容）。
func TestIntegrationSubmitSingleFile(t *testing.T) {
	pool := testPool(t)
	s := testService(t, pool)
	author := mkAuthor(t, pool, "single")

	pkg := writeFile(t, "index.html", "<html>single</html>")
	res, err := s.Submit(context.Background(), SubmitInput{
		AuthorID:    author,
		Title:       "单文件作品",
		DemoType:    "web",
		PackagePath: pkg,
		PackageSize: fileSize(t, pkg),
	})
	if err != nil {
		t.Fatalf("投稿失败: %v", err)
	}
	if _, err := s.files.Stat(context.Background(), storage.FilesKey(res.Slug, 1, "index.html")); err != nil {
		t.Fatalf("单文件未落盘: %v", err)
	}
	if _, err := s.files.Stat(context.Background(), storage.SourceKey(res.Slug, 1, "zip")); err == nil {
		t.Fatal("单文件投稿不该额外存原始包")
	}
}

// 坏包必须 422 且**不留行**：解压失败发生在落库之前，所以库里什么都不该有。
func TestIntegrationSubmitBadPackageLeavesNoRow(t *testing.T) {
	pool := testPool(t)
	s := testService(t, pool)
	author := mkAuthor(t, pool, "bad")

	// 缺 index.html。
	pkg := buildZip(t, map[string]string{"readme.txt": "no entry"})
	_, err := s.Submit(context.Background(), SubmitInput{
		AuthorID:    author,
		Title:       "没有入口",
		DemoType:    "web",
		PackagePath: pkg,
		PackageSize: fileSize(t, pkg),
	})
	if err == nil {
		t.Fatal("缺 index.html 应报错")
	}
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM demos WHERE author_id = $1`, author).Scan(&n); err != nil {
		t.Fatalf("查库失败: %v", err)
	}
	if n != 0 {
		t.Fatalf("坏包不该留下作品行，实际 %d 条", n)
	}
}

// 未知类型由 demo_types 外键兜底，必须报错且不落行。
func TestIntegrationSubmitUnknownType(t *testing.T) {
	pool := testPool(t)
	s := testService(t, pool)
	author := mkAuthor(t, pool, "type")

	pkg := buildZip(t, map[string]string{"index.html": "<html></html>"})
	_, err := s.Submit(context.Background(), SubmitInput{
		AuthorID:    author,
		Title:       "未知类型",
		DemoType:    "video",
		PackagePath: pkg,
		PackageSize: fileSize(t, pkg),
	})
	if err == nil {
		t.Fatal("未知作品类型应报错")
	}
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM demos WHERE author_id = $1`, author).Scan(&n); err != nil {
		t.Fatalf("查库失败: %v", err)
	}
	if n != 0 {
		t.Fatalf("失败投稿不该留下作品行，实际 %d 条", n)
	}
}

// 同名标题重复投稿必须拿到不同的 slug，且两次都成功。
func TestIntegrationSubmitSlugCollision(t *testing.T) {
	pool := testPool(t)
	s := testService(t, pool)
	author := mkAuthor(t, pool, "slug")

	pkg := buildZip(t, map[string]string{"index.html": "<html></html>"})
	var slugs []string
	for i := 0; i < 2; i++ {
		res, err := s.Submit(context.Background(), SubmitInput{
			AuthorID:    author,
			Title:       "同名作品",
			DemoType:    "web",
			PackagePath: pkg,
			PackageSize: fileSize(t, pkg),
		})
		if err != nil {
			t.Fatalf("第 %d 次投稿失败: %v", i+1, err)
		}
		slugs = append(slugs, res.Slug)
	}
	if slugs[0] == slugs[1] {
		t.Fatalf("两次投稿拿到了同一个 slug: %q", slugs[0])
	}
}

func fileSize(t *testing.T, path string) int64 {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat 失败: %v", err)
	}
	return info.Size()
}

// mkReviewer 建一个审核员：accounts（role=moderator）+ 对应的 users 投影行。
// 返回 ReviewActor 需要的两个 id——audit_log 记 accounts.id，demo_timeline 记 users.id。
func mkReviewer(t *testing.T, pool *pgxpool.Pool, tag string) (int64, int64) {
	t.Helper()
	sub := fmt.Sprintf("it-mod-%s", tag)
	var accountID int64
	if err := pool.QueryRow(context.Background(),
		`INSERT INTO accounts (sub, status, role, level)
		 VALUES ($1, 'active', 'moderator', 0) RETURNING id`, sub,
	).Scan(&accountID); err != nil {
		t.Fatalf("建审核员失败: %v", err)
	}
	var userID int64
	if err := pool.QueryRow(context.Background(),
		`INSERT INTO users (sub) VALUES ($1) RETURNING id`, sub,
	).Scan(&userID); err != nil {
		t.Fatalf("建审核员投影失败: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM accounts WHERE id = $1`, accountID)
	})
	return accountID, userID
}

// submitOne 造一条待审作品，返回 slug。
func submitOne(t *testing.T, s *Service, author int64, tag string) string {
	t.Helper()
	pkg := buildZip(t, map[string]string{"index.html": "<html>" + tag + "</html>"})
	res, err := s.Submit(context.Background(), SubmitInput{
		AuthorID:    author,
		Title:       "待审作品-" + tag,
		Description: "描述",
		DemoType:    "web",
		PackagePath: pkg,
		PackageSize: fileSize(t, pkg),
	})
	if err != nil {
		t.Fatalf("投稿失败: %v", err)
	}
	return res.Slug
}

func TestIntegrationReviewApproveWritesBothTraces(t *testing.T) {
	pool := testPool(t)
	s := testService(t, pool)
	author := mkAuthor(t, pool, "rev-a")
	accountID, userID := mkReviewer(t, pool, "a")
	slug := submitOne(t, s, author, "a")

	got, err := s.Review(context.Background(), ReviewActor{
		AccountID: accountID, UserID: userID, Role: "moderator",
	}, ReviewInput{Slug: slug, Approve: true, Reason: "内容正常"})
	if err != nil {
		t.Fatalf("裁决失败: %v", err)
	}
	if got.ModerationState != "approved" {
		t.Fatalf("moderation_state = %q, want approved", got.ModerationState)
	}

	// 两处留痕都必须写：状态改了但没人知道是谁改的，比没改更难查。
	var kind, message string
	var actorID int64
	if err := pool.QueryRow(context.Background(),
		`SELECT t.kind, COALESCE(t.message,''), t.actor_id
		   FROM demo_timeline t JOIN demos d ON d.id = t.demo_id
		  WHERE d.slug = $1`, slug,
	).Scan(&kind, &message, &actorID); err != nil {
		t.Fatalf("时间线没写: %v", err)
	}
	if kind != "approve" || message != "内容正常" || actorID != userID {
		t.Fatalf("时间线不对: kind=%q message=%q actor=%d", kind, message, actorID)
	}

	var action, actorRole, reason string
	var after []byte
	if err := pool.QueryRow(context.Background(),
		`SELECT l.action, l.actor_role, COALESCE(l.reason,''), l.after
		   FROM audit_log l JOIN demos d ON d.id = l.entity_id
		  WHERE l.entity_type = 'demo' AND d.slug = $1`, slug,
	).Scan(&action, &actorRole, &reason, &after); err != nil {
		t.Fatalf("审计没写: %v", err)
	}
	if action != "review" || actorRole != "moderator" || reason != "内容正常" {
		t.Fatalf("审计不对: action=%q role=%q reason=%q", action, actorRole, reason)
	}
	if !strings.Contains(string(after), `"approved"`) {
		t.Fatalf("审计 after 快照应含 approved，实际 %s", after)
	}
}

func TestIntegrationReviewReject(t *testing.T) {
	pool := testPool(t)
	s := testService(t, pool)
	author := mkAuthor(t, pool, "rev-r")
	accountID, userID := mkReviewer(t, pool, "r")
	slug := submitOne(t, s, author, "r")

	got, err := s.Review(context.Background(), ReviewActor{
		AccountID: accountID, UserID: userID, Role: "moderator",
	}, ReviewInput{Slug: slug, Approve: false, Reason: "缺少说明"})
	if err != nil {
		t.Fatalf("裁决失败: %v", err)
	}
	if got.ModerationState != "rejected" {
		t.Fatalf("moderation_state = %q, want rejected", got.ModerationState)
	}
}

func TestIntegrationReviewNotPending(t *testing.T) {
	pool := testPool(t)
	s := testService(t, pool)
	author := mkAuthor(t, pool, "rev-n")
	accountID, userID := mkReviewer(t, pool, "n")
	slug := submitOne(t, s, author, "n")

	actor := ReviewActor{AccountID: accountID, UserID: userID, Role: "moderator"}
	if _, err := s.Review(context.Background(), actor, ReviewInput{Slug: slug, Approve: true}); err != nil {
		t.Fatalf("首次裁决应成功: %v", err)
	}
	// 第二次必须 409 那一档：已经裁决过的作品不能再裁。
	if _, err := s.Review(context.Background(), actor, ReviewInput{Slug: slug, Approve: false}); !errors.Is(err, ErrNotPending) {
		t.Fatalf("expected ErrNotPending, got %v", err)
	}
}

func TestIntegrationReviewNotFound(t *testing.T) {
	pool := testPool(t)
	s := testService(t, pool)
	accountID, userID := mkReviewer(t, pool, "x")

	_, err := s.Review(context.Background(), ReviewActor{
		AccountID: accountID, UserID: userID, Role: "moderator",
	}, ReviewInput{Slug: "does-not-exist", Approve: true})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestIntegrationReviewReasonTooLong(t *testing.T) {
	pool := testPool(t)
	s := testService(t, pool)
	author := mkAuthor(t, pool, "rev-l")
	accountID, userID := mkReviewer(t, pool, "l")
	slug := submitOne(t, s, author, "l")

	_, err := s.Review(context.Background(), ReviewActor{
		AccountID: accountID, UserID: userID, Role: "moderator",
	}, ReviewInput{Slug: slug, Approve: false, Reason: strings.Repeat("字", reviewReasonMax+1)})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput, got %v", err)
	}
	// 校验发生在开事务之前，所以作品应当还停在 pending。
	var state string
	if err := pool.QueryRow(context.Background(),
		`SELECT moderation_state FROM demos WHERE slug = $1`, slug).Scan(&state); err != nil {
		t.Fatalf("查状态失败: %v", err)
	}
	if state != "pending" {
		t.Fatalf("超长说明不该改动状态，实际 %q", state)
	}
}

func TestIntegrationListPendingReviews(t *testing.T) {
	pool := testPool(t)
	s := testService(t, pool)
	author := mkAuthor(t, pool, "rev-q")
	slug := submitOne(t, s, author, "q")

	rows, err := s.ListPendingReviews(context.Background(), 50)
	if err != nil {
		t.Fatalf("查队列失败: %v", err)
	}
	found := false
	for _, r := range rows {
		if r.Slug == slug {
			found = true
			if r.CurrentRevNo != 1 {
				t.Fatalf("current_rev_no = %d, want 1", r.CurrentRevNo)
			}
			if r.Title == "" {
				t.Fatal("标题不该为空")
			}
		}
	}
	if !found {
		t.Fatalf("队列里没找到刚投稿的 %s", slug)
	}
}
