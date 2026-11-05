package demo

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// 生命周期的集成测试。
//
// 这一组钉的是一条容易被误解的边界：**下架与删除是两层**。
// 下架改可见性层（还留在库里、有墓碑页、能重新上架）；
// 删除只写删除标记层（对外不存在、窗口内能恢复）。
// 两者可以叠加：下架之后又删了，恢复回来还是下架态。

// TestIntegrationArchiveAndUnarchive 下架后重新上架，作品回到原来的可见性。
func TestIntegrationArchiveAndUnarchive(t *testing.T) {
	pool := testPool(t)
	s := testService(t, pool)
	author := mkAuthor(t, pool, "life-arch")
	slug := submitForUpdate(t, pool, s, author, "下架测试")
	approveAndPublish(t, pool, slug)
	setChannelVisibility(t, pool, slug, "listed")

	ctx := context.Background()
	result, err := s.Archive(ctx, LifecycleInput{ActorID: author, Slug: slug})
	if err != nil {
		t.Fatalf("下架失败: %v", err)
	}
	if result.LifecycleState != "archived" {
		t.Fatalf("下架后状态 = %q，想要 archived", result.LifecycleState)
	}
	if got := lifecycleStateOf(t, pool, slug); got != "archived" {
		t.Fatalf("库里状态 = %q，想要 archived", got)
	}
	// 下架不动渠道：重新上架后渠道还在原来的可见性上。
	if got := channelVisibilityOf(t, pool, slug); got != "listed" {
		t.Fatalf("下架把渠道改了：%q", got)
	}

	result, err = s.Unarchive(ctx, LifecycleInput{ActorID: author, Slug: slug})
	if err != nil {
		t.Fatalf("重新上架失败: %v", err)
	}
	if result.LifecycleState != "published" {
		t.Fatalf("上架后状态 = %q，想要 published", result.LifecycleState)
	}
	if got := channelVisibilityOf(t, pool, slug); got != "listed" {
		t.Fatalf("重新上架把渠道改了：%q", got)
	}
}

// TestIntegrationArchiveRejectsNonAuthor 非作者连「这个 slug 存不存在」都不该知道。
func TestIntegrationArchiveRejectsNonAuthor(t *testing.T) {
	pool := testPool(t)
	s := testService(t, pool)
	author := mkAuthor(t, pool, "life-other")
	other := mkAuthor(t, pool, "life-other2")
	slug := submitForUpdate(t, pool, s, author, "别人的作品")
	approveAndPublish(t, pool, slug)

	_, err := s.Archive(context.Background(), LifecycleInput{ActorID: other, Slug: slug})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("期望 ErrNotFound，实际 %v", err)
	}
}

// TestIntegrationSoftDeleteAndRestore 删除写两个地方（标记 + 队列行），恢复清两个地方。
func TestIntegrationSoftDeleteAndRestore(t *testing.T) {
	pool := testPool(t)
	s := testService(t, pool)
	author := mkAuthor(t, pool, "life-del")
	slug := submitForUpdate(t, pool, s, author, "删除测试")
	approveAndPublish(t, pool, slug)

	ctx := context.Background()
	before := time.Now()
	result, err := s.SoftDelete(ctx, LifecycleInput{ActorID: author, Slug: slug, Reason: "写错了"})
	if err != nil {
		t.Fatalf("删除失败: %v", err)
	}

	// 窗口在删除那一刻定型：默认 30 天，允许几秒误差。
	want := before.Add(30 * 24 * time.Hour)
	if diff := result.PurgeAfter.Sub(want); diff < -5*time.Second || diff > 5*time.Second {
		t.Fatalf("purgeAfter 与「删除时刻 + 30 天」差 %v", diff)
	}
	if got := deletedReasonOf(t, pool, slug); got != "写错了" {
		t.Fatalf("删除理由 = %q", got)
	}
	if got := gcRowCount(t, pool, slug); got != 1 {
		t.Fatalf("队列行数 = %d，想要 1", got)
	}
	// 关键一条：软删**不改可见性层**。删除前是 published，删完还是 published。
	if got := lifecycleStateOf(t, pool, slug); got != "published" {
		t.Fatalf("软删动了可见性层：%q", got)
	}

	if _, err := s.RestoreDeleted(ctx, LifecycleInput{ActorID: author, Slug: slug}); err != nil {
		t.Fatalf("恢复失败: %v", err)
	}
	if got := deletedReasonOf(t, pool, slug); got != "" {
		t.Fatalf("恢复后删除理由没清：%q", got)
	}
	if got := gcRowCount(t, pool, slug); got != 0 {
		t.Fatalf("恢复后队列行还在：%d", got)
	}
}

// TestIntegrationRestoreWindowClosed 窗口一过就不给恢复。
//
// 直接把队列行的 purge_after 拨到过去——这就是「窗口在删除那一刻定型」的好处：
// 判据只有一行时间戳，测试不需要等 30 天，也不需要动系统时钟。
func TestIntegrationRestoreWindowClosed(t *testing.T) {
	pool := testPool(t)
	s := testService(t, pool)
	author := mkAuthor(t, pool, "life-window")
	slug := submitForUpdate(t, pool, s, author, "过期恢复")
	approveAndPublish(t, pool, slug)

	ctx := context.Background()
	if _, err := s.SoftDelete(ctx, LifecycleInput{ActorID: author, Slug: slug}); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`UPDATE gc_queue SET purge_after = now() - interval '1 hour' WHERE object_key = $1`,
		demoDirKey(slug)); err != nil {
		t.Fatalf("拨窗口失败: %v", err)
	}

	_, err := s.RestoreDeleted(ctx, LifecycleInput{ActorID: author, Slug: slug})
	if !errors.Is(err, ErrRestoreWindowClosed) {
		t.Fatalf("期望 ErrRestoreWindowClosed，实际 %v", err)
	}
	if got := gcRowCount(t, pool, slug); got != 1 {
		t.Fatalf("窗口关了却把队列行删了：%d", got)
	}
}

// TestIntegrationRestoreWindowFollowsSetting 窗口值来自设置域，不是写死的 30。
func TestIntegrationRestoreWindowFollowsSetting(t *testing.T) {
	pool := testPool(t)
	s := testService(t, pool)
	author := mkAuthor(t, pool, "life-setting")
	slug := submitForUpdate(t, pool, s, author, "参数化窗口")
	approveAndPublish(t, pool, slug)

	ctx := context.Background()
	// 覆盖站点参数为 7 天，测完恢复原样（别的用例假定默认值）。
	if _, err := pool.Exec(ctx, `
		INSERT INTO site_settings (key, scope, value_type, value)
		VALUES ('lifecycle.restore_window_days', 'site', 'number', '7'::jsonb)
		ON CONFLICT (key) DO UPDATE SET value = '7'::jsonb`); err != nil {
		t.Fatalf("写参数失败: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM site_settings WHERE key = 'lifecycle.restore_window_days'`)
	})

	before := time.Now()
	result, err := s.SoftDelete(ctx, LifecycleInput{ActorID: author, Slug: slug})
	if err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	want := before.Add(7 * 24 * time.Hour)
	if diff := result.PurgeAfter.Sub(want); diff < -5*time.Second || diff > 5*time.Second {
		t.Fatalf("purgeAfter 没用设置里的 7 天，与实际差 %v", diff)
	}
}

// TestIntegrationSoftDeleteRejectsRemoved 平台移除的作品，作者不能自己删（也就不能自己恢复）。
func TestIntegrationSoftDeleteRejectsRemoved(t *testing.T) {
	pool := testPool(t)
	s := testService(t, pool)
	author := mkAuthor(t, pool, "life-removed")
	slug := submitForUpdate(t, pool, s, author, "被移除的作品")
	approveAndPublish(t, pool, slug)

	if _, err := pool.Exec(context.Background(),
		`UPDATE demos SET lifecycle_state = 'removed' WHERE slug = $1`, slug); err != nil {
		t.Fatalf("造移除态失败: %v", err)
	}
	_, err := s.SoftDelete(context.Background(), LifecycleInput{ActorID: author, Slug: slug})
	if !errors.Is(err, ErrNotEditable) {
		t.Fatalf("期望 ErrNotEditable，实际 %v", err)
	}
}

// TestIntegrationDeleteRejectsTwice 删两次是冲突，不是幂等成功。
//
// 幂等成功会让第二次删除把 purge_after 往后推——等于「多点几次就能无限延期」。
func TestIntegrationDeleteRejectsTwice(t *testing.T) {
	pool := testPool(t)
	s := testService(t, pool)
	author := mkAuthor(t, pool, "life-twice")
	slug := submitForUpdate(t, pool, s, author, "删两次")
	approveAndPublish(t, pool, slug)

	ctx := context.Background()
	if _, err := s.SoftDelete(ctx, LifecycleInput{ActorID: author, Slug: slug}); err != nil {
		t.Fatalf("第一次删除失败: %v", err)
	}
	_, err := s.SoftDelete(ctx, LifecycleInput{ActorID: author, Slug: slug})
	if !errors.Is(err, ErrLifecycleConflict) {
		t.Fatalf("第二次删除期望 ErrLifecycleConflict，实际 %v", err)
	}
}

// ---- 小工具 ----

func lifecycleStateOf(t *testing.T, pool *pgxpool.Pool, slug string) string {
	t.Helper()
	var state string
	if err := pool.QueryRow(context.Background(),
		`SELECT lifecycle_state FROM demos WHERE slug = $1`, slug).Scan(&state); err != nil {
		t.Fatalf("读作品状态失败: %v", err)
	}
	return state
}

func setChannelVisibility(t *testing.T, pool *pgxpool.Pool, slug, visibility string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`UPDATE demo_channels c SET visibility = $2
		 FROM demos d WHERE c.demo_id = d.id AND d.slug = $1`, slug, visibility); err != nil {
		t.Fatalf("改渠道可见性失败: %v", err)
	}
}

func channelVisibilityOf(t *testing.T, pool *pgxpool.Pool, slug string) string {
	t.Helper()
	var v string
	if err := pool.QueryRow(context.Background(),
		`SELECT c.visibility FROM demo_channels c JOIN demos d ON d.id = c.demo_id WHERE d.slug = $1`,
		slug).Scan(&v); err != nil {
		t.Fatalf("读渠道可见性失败: %v", err)
	}
	return v
}

func deletedReasonOf(t *testing.T, pool *pgxpool.Pool, slug string) string {
	t.Helper()
	var reason string
	if err := pool.QueryRow(context.Background(),
		`SELECT delete_reason FROM demos WHERE slug = $1`, slug).Scan(&reason); err != nil {
		t.Fatalf("读删除理由失败: %v", err)
	}
	return reason
}

func gcRowCount(t *testing.T, pool *pgxpool.Pool, slug string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM gc_queue WHERE object_key = $1`, demoDirKey(slug)).Scan(&n); err != nil {
		t.Fatalf("数队列行失败: %v", err)
	}
	return n
}
