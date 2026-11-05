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
