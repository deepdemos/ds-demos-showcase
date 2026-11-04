package demo

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// 发布链路的行为钉死。这几条各自对应一个会被误判成「代码坏了」的真实场景：
// 没审过就发、发两次、渠道写错、作品已被移除。
func approvedOne(t *testing.T, s *Service, pool *pgxpool.Pool, tag string) string {
	t.Helper()
	author := mkAuthor(t, pool, tag)
	accountID, userID := mkReviewer(t, pool, tag+"-rev")
	slug := submitOne(t, s, author, tag)
	if _, err := s.Review(context.Background(), ReviewActor{
		AccountID: accountID, UserID: userID, Role: "moderator",
	}, ReviewInput{Slug: slug, Approve: true, Reason: "内容正常"}); err != nil {
		t.Fatalf("裁决失败: %v", err)
	}
	return slug
}

func publisher(t *testing.T, pool *pgxpool.Pool, tag string) ReviewActor {
	t.Helper()
	accountID, userID := mkReviewer(t, pool, tag+"-pub")
	if _, err := pool.Exec(context.Background(),
		`UPDATE accounts SET role = 'admin' WHERE id = $1`, accountID); err != nil {
		t.Fatalf("提权失败: %v", err)
	}
	return ReviewActor{AccountID: accountID, UserID: userID, Role: "admin"}
}

func TestIntegrationPublishHappyPath(t *testing.T) {
	pool := testPool(t)
	s := testService(t, pool)
	slug := approvedOne(t, s, pool, "pub-a")
	actor := publisher(t, pool, "pub-a")

	got, err := s.Publish(context.Background(), actor, PublishInput{
		Slug: slug, Channel: "deep", Visibility: "listed",
	})
	if err != nil {
		t.Fatalf("发布失败: %v", err)
	}
	if got.LifecycleState != "published" {
		t.Fatalf("lifecycle = %q, want published", got.LifecycleState)
	}

	var lifecycle, channel, visibility string
	if err := pool.QueryRow(context.Background(),
		`SELECT d.lifecycle_state, c.channel, c.visibility
		 FROM demos d JOIN demo_channels c ON c.demo_id = d.id
		 WHERE d.slug = $1`, slug,
	).Scan(&lifecycle, &channel, &visibility); err != nil {
		t.Fatalf("查落库结果失败: %v", err)
	}
	if lifecycle != "published" || channel != "deep" || visibility != "listed" {
		t.Fatalf("落库 = %s/%s/%s, want published/deep/listed", lifecycle, channel, visibility)
	}

	// 两处留痕都要有：时间线给人看，审计给追责用。
	var timeline, audit int
	if err := pool.QueryRow(context.Background(),
		`SELECT (SELECT count(*) FROM demo_timeline WHERE demo_id = d.id AND kind = 'publish'),
		        (SELECT count(*) FROM audit_log WHERE entity_type = 'demo' AND entity_id = d.id AND action = 'status_set')
		 FROM demos d WHERE d.slug = $1`, slug,
	).Scan(&timeline, &audit); err != nil {
		t.Fatalf("查留痕失败: %v", err)
	}
	if timeline != 1 || audit != 1 {
		t.Fatalf("留痕 = timeline %d / audit %d, want 1/1", timeline, audit)
	}
}

func TestIntegrationPublishRequiresApproval(t *testing.T) {
	pool := testPool(t)
	s := testService(t, pool)
	author := mkAuthor(t, pool, "pub-pend")
	actor := publisher(t, pool, "pub-pend")
	slug := submitOne(t, s, author, "pub-pend")

	_, err := s.Publish(context.Background(), actor, PublishInput{
		Slug: slug, Channel: "deep", Visibility: "listed",
	})
	if !errors.Is(err, ErrNotApproved) {
		t.Fatalf("未过审应 ErrNotApproved，实际 %v", err)
	}
}

func TestIntegrationPublishTwiceIsConflict(t *testing.T) {
	pool := testPool(t)
	s := testService(t, pool)
	slug := approvedOne(t, s, pool, "pub-twice")
	actor := publisher(t, pool, "pub-twice")

	if _, err := s.Publish(context.Background(), actor, PublishInput{
		Slug: slug, Channel: "deep", Visibility: "listed",
	}); err != nil {
		t.Fatalf("第一次发布失败: %v", err)
	}
	_, err := s.Publish(context.Background(), actor, PublishInput{
		Slug: slug, Channel: "deep", Visibility: "listed",
	})
	if !errors.Is(err, ErrAlreadyPublished) {
		t.Fatalf("重复发布应 ErrAlreadyPublished，实际 %v", err)
	}
}

func TestIntegrationPublishRejectsBadInput(t *testing.T) {
	pool := testPool(t)
	s := testService(t, pool)
	slug := approvedOne(t, s, pool, "pub-bad")
	actor := publisher(t, pool, "pub-bad")

	if _, err := s.Publish(context.Background(), actor, PublishInput{
		Slug: slug, Channel: "no-such-channel", Visibility: "listed",
	}); !errors.Is(err, ErrInvalidChannel) {
		t.Fatalf("未登记渠道应 ErrInvalidChannel，实际 %v", err)
	}
	if _, err := s.Publish(context.Background(), actor, PublishInput{
		Slug: slug, Channel: "deep", Visibility: "everyone",
	}); !errors.Is(err, ErrInvalidVisibility) {
		t.Fatalf("非法可见性应 ErrInvalidVisibility，实际 %v", err)
	}
	// 两次失败都不该留下半截状态：仍然没有渠道行、仍然是 draft。
	var lifecycle string
	var channels int
	if err := pool.QueryRow(context.Background(),
		`SELECT d.lifecycle_state, (SELECT count(*) FROM demo_channels c WHERE c.demo_id = d.id)
		 FROM demos d WHERE d.slug = $1`, slug,
	).Scan(&lifecycle, &channels); err != nil {
		t.Fatalf("查状态失败: %v", err)
	}
	if lifecycle != "draft" || channels != 0 {
		t.Fatalf("失败后状态 = %s / %d 个渠道行, want draft / 0", lifecycle, channels)
	}
}

// Astra 橱窗只收英文作品（Demo 域 §3.5 / §7.4）。
//
// 这条校验以前只活在前端发布表单里（把语言锁成 en 并禁用下拉），后端不判——
// 一道只在浏览器里存在的校验等于没有：curl 一次就能把中文作品发进英文橱窗。
func TestIntegrationPublishAstraRequiresEnglish(t *testing.T) {
	pool := testPool(t)
	s := testService(t, pool)
	slug := approvedOne(t, s, pool, "pub-astra")
	actor := publisher(t, pool, "pub-astra")

	if _, err := s.Publish(context.Background(), actor, PublishInput{
		Slug: slug, Channel: "astra", Visibility: "listed", Lang: "zh",
	}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("astra + zh 应 ErrInvalidInput，实际 %v", err)
	}
	// 失败不该留下半截状态。
	var lifecycle string
	var channels int
	if err := pool.QueryRow(context.Background(),
		`SELECT d.lifecycle_state, (SELECT count(*) FROM demo_channels c WHERE c.demo_id = d.id)
		 FROM demos d WHERE d.slug = $1`, slug,
	).Scan(&lifecycle, &channels); err != nil {
		t.Fatalf("查状态失败: %v", err)
	}
	if lifecycle != "draft" || channels != 0 {
		t.Fatalf("astra 语言不符后状态 = %s / %d 个渠道行, want draft / 0", lifecycle, channels)
	}

	// 英文就放行。
	if _, err := s.Publish(context.Background(), actor, PublishInput{
		Slug: slug, Channel: "astra", Visibility: "listed", Lang: "en",
	}); err != nil {
		t.Fatalf("astra + en 应当成功，实际 %v", err)
	}

	// deep 没有语言要求：中文照发。
	slug2 := approvedOne(t, s, pool, "pub-astra-b")
	if _, err := s.Publish(context.Background(), actor, PublishInput{
		Slug: slug2, Channel: "deep", Visibility: "listed", Lang: "zh",
	}); err != nil {
		t.Fatalf("deep + zh 应当成功，实际 %v", err)
	}
}

func TestIntegrationPublishRemovedIsRejected(t *testing.T) {
	pool := testPool(t)
	s := testService(t, pool)
	slug := approvedOne(t, s, pool, "pub-rm")
	actor := publisher(t, pool, "pub-rm")

	if _, err := pool.Exec(context.Background(),
		`UPDATE demos SET lifecycle_state = 'removed' WHERE slug = $1`, slug); err != nil {
		t.Fatalf("改成 removed 失败: %v", err)
	}
	_, err := s.Publish(context.Background(), actor, PublishInput{
		Slug: slug, Channel: "deep", Visibility: "listed",
	})
	if !errors.Is(err, ErrRemovedCannotPublish) {
		t.Fatalf("已移除作品应 ErrRemovedCannotPublish，实际 %v", err)
	}
}
