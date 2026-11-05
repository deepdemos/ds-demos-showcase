package demo

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// 生命周期的两处留痕。
//
// 这一组钉的是「设计写了、代码里到底有没有人写」：
//   - 治理域 §9.1：作品软删要有一条 `delete` 审计，删的人只存在 `actor_id` 里；
//   - 治理域 §5.2 规则 1：审计与业务变更同事务；
//   - Demo 域 §3.11：时间线只记对外可见的动作——归档与恢复要记，软删不记。
//
// 断言的是**行真的存在且字段对**，不是「函数没报错」：审计最容易漏的一种坏法
// 就是「操作成功了，痕迹没有」，它不会让任何断言变红，只会让事后查不出来。

// demoIDOf 按 slug 取 demos.id（审计与时间线的 entity_id / demo_id 都是它）。
func demoIDOf(t *testing.T, pool *pgxpool.Pool, slug string) int64 {
	t.Helper()
	var id int64
	if err := pool.QueryRow(context.Background(),
		`SELECT id FROM demos WHERE slug = $1`, slug).Scan(&id); err != nil {
		t.Fatalf("取 demo id 失败: %v", err)
	}
	return id
}

// lastAuditOf 读某作品最近一条审计行。
func lastAuditOf(t *testing.T, pool *pgxpool.Pool, demoID int64) (action, before, after, reason string, actorID int64) {
	t.Helper()
	err := pool.QueryRow(context.Background(), `
		SELECT action, COALESCE(before::text, ''), COALESCE(after::text, ''),
		       reason, COALESCE(actor_id, 0)
		  FROM audit_log
		 WHERE entity_type = 'demo' AND entity_id = $1
		 ORDER BY id DESC LIMIT 1`, demoID,
	).Scan(&action, &before, &after, &reason, &actorID)
	if err != nil {
		t.Fatalf("读审计失败: %v", err)
	}
	return action, before, after, reason, actorID
}

// auditCountOf 数某作品有几条审计行。
func auditCountOf(t *testing.T, pool *pgxpool.Pool, demoID int64) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM audit_log WHERE entity_type = 'demo' AND entity_id = $1`, demoID,
	).Scan(&n); err != nil {
		t.Fatalf("数审计行失败: %v", err)
	}
	return n
}

// timelineKindsOf 按写入顺序返回时间线的动作名。
func timelineKindsOf(t *testing.T, pool *pgxpool.Pool, demoID int64) []string {
	t.Helper()
	rows, err := pool.Query(context.Background(),
		`SELECT kind FROM demo_timeline WHERE demo_id = $1 ORDER BY id`, demoID)
	if err != nil {
		t.Fatalf("读时间线失败: %v", err)
	}
	defer rows.Close()
	var kinds []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			t.Fatalf("扫描时间线失败: %v", err)
		}
		kinds = append(kinds, k)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("遍历时间线失败: %v", err)
	}
	return kinds
}

// strMap 把 JSON 快照解成 map，方便断言「存的是关键字段，不是整行」。
func strMap(t *testing.T, raw string) map[string]any {
	t.Helper()
	if raw == "" {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("解审计快照失败（%s）: %v", raw, err)
	}
	return m
}

// TestIntegrationArchiveWritesTrail 下架与重新上架：审计 + 时间线都要有。
func TestIntegrationArchiveWritesTrail(t *testing.T) {
	pool := testPool(t)
	s := testService(t, pool)
	author := mkAuthor(t, pool, "trail-arch")
	actor := actorOf(t, pool, author)
	slug := submitForUpdate(t, pool, s, author, "留痕下架测试")
	approveAndPublish(t, pool, slug)
	demoID := demoIDOf(t, pool, slug)

	if _, err := s.Archive(context.Background(), LifecycleInput{Actor: actor, Slug: slug}); err != nil {
		t.Fatalf("下架失败: %v", err)
	}

	action, before, after, _, auditActor := lastAuditOf(t, pool, demoID)
	if action != "status_set" {
		t.Fatalf("下架的审计动作 = %q，想要 status_set（状态跃迁）", action)
	}
	// 删的人不落在 demos 上，只能看审计行的 actor_id——而它是 accounts.id。
	if auditActor != actor.AccountID {
		t.Fatalf("审计 actor_id = %d，想要 accounts.id %d", auditActor, actor.AccountID)
	}
	if got := strMap(t, before)["lifecycle_state"]; got != "published" {
		t.Fatalf("审计前态 lifecycle_state = %v，想要 published", got)
	}
	if got := strMap(t, after)["lifecycle_state"]; got != "archived" {
		t.Fatalf("审计后态 lifecycle_state = %v，想要 archived", got)
	}
	// 审计只存关键字段：正文、简介、标题一律不许进来（治理域 §5.2 规则 2）。
	for _, forbidden := range []string{"title", "description", "prompt"} {
		if _, ok := strMap(t, after)[forbidden]; ok {
			t.Fatalf("审计快照里出现了内容字段 %q", forbidden)
		}
	}

	if kinds := timelineKindsOf(t, pool, demoID); len(kinds) == 0 || kinds[len(kinds)-1] != "archive" {
		t.Fatalf("下架后时间线 = %v，最后一条想要 archive", kinds)
	}

	// 重新上架：时间线加一条 restore，审计再加一条 status_set。
	if _, err := s.Unarchive(context.Background(), LifecycleInput{Actor: actor, Slug: slug}); err != nil {
		t.Fatalf("重新上架失败: %v", err)
	}
	if kinds := timelineKindsOf(t, pool, demoID); kinds[len(kinds)-1] != "restore" {
		t.Fatalf("上架后时间线 = %v，最后一条想要 restore", kinds)
	}
	action, before, after, _, _ = lastAuditOf(t, pool, demoID)
	if action != "status_set" {
		t.Fatalf("上架的审计动作 = %q，想要 status_set", action)
	}
	if got := strMap(t, after)["lifecycle_state"]; got != "published" {
		t.Fatalf("上架审计后态 = %v，想要 published", got)
	}
}

// TestIntegrationSoftDeleteWritesAuditOnly 软删与恢复删除：只有审计，没有时间线。
//
// 时间线记「对外发生过什么」；软删之后作品对外根本不存在，给它留一条痕迹
// 等于给墓碑开一个「曾经被删过」的漏口（Demo 域 §3.11 的判据）。
func TestIntegrationSoftDeleteWritesAuditOnly(t *testing.T) {
	pool := testPool(t)
	s := testService(t, pool)
	author := mkAuthor(t, pool, "trail-del")
	actor := actorOf(t, pool, author)
	slug := submitForUpdate(t, pool, s, author, "留痕删除测试")
	approveAndPublish(t, pool, slug)
	demoID := demoIDOf(t, pool, slug)

	timelineBefore := len(timelineKindsOf(t, pool, demoID))
	auditBefore := auditCountOf(t, pool, demoID)

	if _, err := s.SoftDelete(context.Background(), LifecycleInput{
		Actor: actor, Slug: slug, Reason: "写错了",
	}); err != nil {
		t.Fatalf("删除失败: %v", err)
	}

	action, before, after, reason, auditActor := lastAuditOf(t, pool, demoID)
	if action != "delete" {
		t.Fatalf("删除的审计动作 = %q，想要 delete", action)
	}
	if reason != "写错了" {
		t.Fatalf("审计理由 = %q，想要「写错了」", reason)
	}
	if auditActor != actor.AccountID {
		t.Fatalf("删除审计 actor_id = %d，想要 %d", auditActor, actor.AccountID)
	}
	if got := strMap(t, before)["deleted"]; got != false {
		t.Fatalf("删除前态 deleted = %v，想要 false", got)
	}
	if got := strMap(t, after)["deleted"]; got != true {
		t.Fatalf("删除后态 deleted = %v，想要 true", got)
	}
	if got := auditCountOf(t, pool, demoID); got != auditBefore+1 {
		t.Fatalf("删除后审计行数 = %d，想要 %d", got, auditBefore+1)
	}
	if got := len(timelineKindsOf(t, pool, demoID)); got != timelineBefore {
		t.Fatalf("软删不该写时间线，行数从 %d 变成 %d", timelineBefore, got)
	}

	// 恢复：审计加一条 restore，时间线仍然不动。
	if _, err := s.RestoreDeleted(context.Background(), LifecycleInput{Actor: actor, Slug: slug}); err != nil {
		t.Fatalf("恢复失败: %v", err)
	}
	action, _, after, _, _ = lastAuditOf(t, pool, demoID)
	if action != "restore" {
		t.Fatalf("恢复的审计动作 = %q，想要 restore", action)
	}
	if got := strMap(t, after)["deleted"]; got != false {
		t.Fatalf("恢复后态 deleted = %v，想要 false", got)
	}
	if got := len(timelineKindsOf(t, pool, demoID)); got != timelineBefore {
		t.Fatalf("恢复删除不该写时间线，行数从 %d 变成 %d", timelineBefore, got)
	}
}

// TestIntegrationLifecycleRefusesEmptyRole 没有角色就不写审计行。
//
// 静默写一行 actor_role=” 的审计，等于留下「谁改的不知道」，
// 比直接失败更难发现——所以宁可整个操作失败。
func TestIntegrationLifecycleRefusesEmptyRole(t *testing.T) {
	pool := testPool(t)
	s := testService(t, pool)
	author := mkAuthor(t, pool, "trail-norole")
	slug := submitForUpdate(t, pool, s, author, "缺角色测试")
	approveAndPublish(t, pool, slug)
	demoID := demoIDOf(t, pool, slug)

	_, err := s.Archive(context.Background(), LifecycleInput{
		Actor: Actor{AccountID: 1, UserID: author, Role: ""},
		Slug:  slug,
	})
	if err == nil {
		t.Fatalf("缺少操作人角色时不该成功")
	}
	if got := lifecycleStateOf(t, pool, slug); got != "published" {
		t.Fatalf("失败的那次不该改状态，实际 %q", got)
	}
	if got := auditCountOf(t, pool, demoID); got != 0 {
		t.Fatalf("失败的那次不该留审计，实际 %d 行", got)
	}
}
