package demo

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// 时间顺序约束（ck_demos_time_order）的回归测试。
//
// # 为什么会有这几条
//
// 写点用的是 now()，它是**事务开始时间**，不是「这条语句真正跑的时刻」。库所在
// 机器的时钟被 NTP 往回拨之后，相邻两条语句里后一条的 now() 可能比前一条刚写下
// 的 created_at 还早，于是「更新自己刚写的行」撞上
// `updated_at >= created_at` —— 对外表现是审核接口偶发 503，日志里一行都没有
// （writeReviewError 的 default 分支当时不写日志）。
//
// 修法在 SQL 侧：写 updated_at 时取 GREATEST(now(), created_at)，写 deleted_at 时
// 同样以 created_at 为下限。约束保持不变——它仍然是真判据，只是不再被时钟回拨
// 误伤。
//
// 这几条测试不依赖机器时钟真的被拨回去：直接把这一行的时间戳推到未来十分钟，
// 之后任何 now() 都必然小于它，正是线上那一幕。

// pushTimeForward 把作品的时间戳推到未来 10 分钟，造出「时钟回拨」的现场。
//
// deleted_at 一并推：否则「已经软删过的行」在推完之后 deleted_at 反而早于
// created_at，测试自己在造现场这一步就先撞约束了（这正是软删也要夹紧的理由）。
func pushTimeForward(t *testing.T, pool *pgxpool.Pool, slug string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`UPDATE demos SET created_at = now() + interval '10 minutes',
		                   updated_at = now() + interval '10 minutes',
		                   deleted_at = CASE WHEN deleted_at IS NULL THEN NULL
		                                     ELSE now() + interval '10 minutes' END
		 WHERE slug = $1`, slug); err != nil {
		t.Fatalf("造时钟回拨现场失败: %v", err)
	}
}

// assertTimeOrder 直接验约束自己：更新过了，而三条时间关系仍然成立。
func assertTimeOrder(t *testing.T, pool *pgxpool.Pool, slug string) {
	t.Helper()
	var ok bool
	if err := pool.QueryRow(context.Background(),
		`SELECT updated_at >= created_at
		    AND (deleted_at IS NULL OR deleted_at >= created_at)
		   FROM demos WHERE slug = $1`, slug).Scan(&ok); err != nil {
		t.Fatalf("读时间戳失败: %v", err)
	}
	if !ok {
		t.Fatalf("时间顺序被破坏：updated_at/deleted_at 早于 created_at")
	}
}

func TestIntegrationReviewSurvivesClockStepBack(t *testing.T) {
	pool := testPool(t)
	s := testService(t, pool)
	author := mkAuthor(t, pool, "clock-rev")
	accountID, userID := mkReviewer(t, pool, "clock-rev")
	slug := submitOne(t, s, author, "clock-rev")

	pushTimeForward(t, pool, slug)

	if _, err := s.Review(context.Background(), Actor{
		AccountID: accountID, UserID: userID, Role: "moderator",
	}, ReviewInput{Slug: slug, Approve: true, Reason: "时钟回拨也不该 503"}); err != nil {
		t.Fatalf("时钟回拨后裁决失败（写点没夹紧？）: %v", err)
	}
	assertTimeOrder(t, pool, slug)
}

func TestIntegrationSoftDeleteSurvivesClockStepBack(t *testing.T) {
	pool := testPool(t)
	s := testService(t, pool)
	author := mkAuthor(t, pool, "clock-del")
	slug := submitOne(t, s, author, "clock-del")

	pushTimeForward(t, pool, slug)

	if _, err := s.SoftDelete(context.Background(), LifecycleInput{
		Actor: actorOf(t, pool, author), Slug: slug, Reason: "时钟回拨也不该 503",
	}); err != nil {
		t.Fatalf("时钟回拨后软删失败（deleted_at 没夹紧？）: %v", err)
	}
	assertTimeOrder(t, pool, slug)
}

func TestIntegrationRestoreSurvivesClockStepBack(t *testing.T) {
	pool := testPool(t)
	s := testService(t, pool)
	author := mkAuthor(t, pool, "clock-rst")
	slug := submitOne(t, s, author, "clock-rst")
	actor := actorOf(t, pool, author)

	if _, err := s.SoftDelete(context.Background(), LifecycleInput{
		Actor: actor, Slug: slug, Reason: "先删",
	}); err != nil {
		t.Fatalf("软删失败: %v", err)
	}
	pushTimeForward(t, pool, slug)

	if _, err := s.RestoreDeleted(context.Background(), LifecycleInput{
		Actor: actor, Slug: slug,
	}); err != nil {
		t.Fatalf("时钟回拨后恢复失败（写点没夹紧？）: %v", err)
	}
	assertTimeOrder(t, pool, slug)
}

// pushChannelTimeForward 把渠道行的时间戳推到未来 10 分钟。
//
// demo_channels 与 demos 一样有 ck_channel_time（updated_at >= created_at、
// published_at 为空或 >= created_at）。发布走的是 UPSERT，冲突分支写
// updated_at = GREATEST(now(), created_at)——这条用例钉的正是那次漏改。
func pushChannelTimeForward(t *testing.T, pool *pgxpool.Pool, slug string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`UPDATE demo_channels
		    SET created_at = now() + interval '10 minutes',
		        updated_at = now() + interval '10 minutes',
		        published_at = CASE WHEN published_at IS NULL THEN NULL
		                            ELSE now() + interval '10 minutes' END
		  WHERE demo_id = (SELECT id FROM demos WHERE slug = $1)`, slug); err != nil {
		t.Fatalf("造渠道时钟回拨现场失败: %v", err)
	}
}

// 0013 回填的老作品，渠道行 published_at 是 NULL。首次重发布走的是
// COALESCE(旧值, EXCLUDED.published_at) 的**右边**——那条路径上 EXCLUDED 就是裸
// now()，回拨窗口里它会早于渠道行自己的 created_at，撞 ck_channel_time。
//
// 上面那条用例抓不到这一格：它先发布过一次，published_at 已非 NULL，COALESCE 走左边。
func TestIntegrationFirstRepublishAfterBackfillSurvivesClockStepBack(t *testing.T) {
	pool := testPool(t)
	s := testService(t, pool)
	ctx := context.Background()

	author := mkAuthor(t, pool, "clock-null")
	reviewerAccount, reviewerUser := mkReviewer(t, pool, "clock-null-rev")
	slug := submitOne(t, s, author, "clock-null")
	if _, err := s.Review(ctx, Actor{
		AccountID: reviewerAccount, UserID: reviewerUser, Role: "moderator",
	}, ReviewInput{Slug: slug, Approve: true, Reason: "内容正常"}); err != nil {
		t.Fatalf("裁决失败: %v", err)
	}
	actor := publisher(t, pool, "clock-null")

	// 直接造出 0013 回填之后的现场：有渠道行、published_at 为 NULL、时间戳在未来。
	if _, err := s.Publish(ctx, actor, PublishInput{Slug: slug, Channel: "deep", Visibility: "listed"}); err != nil {
		t.Fatalf("首次发布失败: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`UPDATE demo_channels
		    SET published_at = NULL,
		        created_at = now() + interval '10 minutes',
		        updated_at = now() + interval '10 minutes'
		  WHERE demo_id = (SELECT id FROM demos WHERE slug = $1)`, slug); err != nil {
		t.Fatalf("造回填现场失败: %v", err)
	}

	authorActor := actorOf(t, pool, author)
	if _, err := s.Archive(ctx, LifecycleInput{Actor: authorActor, Slug: slug}); err != nil {
		t.Fatalf("归档失败: %v", err)
	}
	if _, err := s.Publish(ctx, actor, PublishInput{Slug: slug, Channel: "deep", Visibility: "unlisted"}); err != nil {
		t.Fatalf("回填行在时钟回拨下重新发布失败（published_at 没夹紧？）: %v", err)
	}

	var ok bool
	if err := pool.QueryRow(ctx,
		`SELECT c.published_at IS NOT NULL AND c.published_at >= c.created_at
		   FROM demo_channels c WHERE c.demo_id = (SELECT id FROM demos WHERE slug = $1)`, slug).Scan(&ok); err != nil {
		t.Fatalf("读渠道时间戳失败: %v", err)
	}
	if !ok {
		t.Fatalf("首次写入的 published_at 早于渠道行 created_at：ck_channel_time 是这么被撞的")
	}
}

// 下架再重新发布会走到 UpsertDemoChannel 的冲突分支——那是第九个写时间的地方，
// df60f09 只改了 demos 的八处，这一处漏了。表现与当年那批一模一样：发布接口 503。
func TestIntegrationRepublishSurvivesChannelClockStepBack(t *testing.T) {
	pool := testPool(t)
	s := testService(t, pool)
	ctx := context.Background()

	// 发布是管理员动作、归档是作者动作，两个身份都得有，所以不能直接用 approvedOne。
	author := mkAuthor(t, pool, "clock-ch")
	reviewerAccount, reviewerUser := mkReviewer(t, pool, "clock-ch-rev")
	slug := submitOne(t, s, author, "clock-ch")
	if _, err := s.Review(ctx, Actor{
		AccountID: reviewerAccount, UserID: reviewerUser, Role: "moderator",
	}, ReviewInput{Slug: slug, Approve: true, Reason: "内容正常"}); err != nil {
		t.Fatalf("裁决失败: %v", err)
	}
	actor := publisher(t, pool, "clock-ch")
	authorActor := actorOf(t, pool, author)

	if _, err := s.Publish(ctx, actor, PublishInput{Slug: slug, Channel: "deep", Visibility: "listed"}); err != nil {
		t.Fatalf("首次发布失败: %v", err)
	}

	pushChannelTimeForward(t, pool, slug)

	// 归档之后才允许再发布：PublishDemo 的 WHERE 只认 draft / archived。
	if _, err := s.Archive(ctx, LifecycleInput{Actor: authorActor, Slug: slug}); err != nil {
		t.Fatalf("归档失败: %v", err)
	}

	if _, err := s.Publish(ctx, actor, PublishInput{Slug: slug, Channel: "deep", Visibility: "unlisted"}); err != nil {
		t.Fatalf("时钟回拨后重新发布失败（渠道写点没夹紧？）: %v", err)
	}

	var ok bool
	if err := pool.QueryRow(ctx,
		`SELECT c.updated_at >= c.created_at AND (c.published_at IS NULL OR c.published_at >= c.created_at)
		   FROM demo_channels c WHERE c.demo_id = (SELECT id FROM demos WHERE slug = $1)`, slug).Scan(&ok); err != nil {
		t.Fatalf("读渠道时间戳失败: %v", err)
	}
	if !ok {
		t.Fatalf("渠道时间顺序被破坏：updated_at / published_at 早于 created_at")
	}
}
