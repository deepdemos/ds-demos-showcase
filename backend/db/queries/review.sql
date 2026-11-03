-- 审核裁决（治理域 §7「审核契约」+ Demo 域 §7.3）。
--
-- 为什么在这里而不是 demos.sql：审核改的是 demos.moderation_state，但它的语义与
-- 留痕口径由治理域定义（review_kinds / audit_log）。放在单独文件里，
-- 以后 proposals 接入同一套审核时能一眼看出「哪些查询是审核共用的」。

-- name: ListPendingDemoReviews :many
-- 待审队列：只列 pending，按提交时间正序（先来先审）。
--
-- 作者名走 users → accounts → profiles 三跳：users 是 demo 站的投影表，
-- 档案在 profiles，而 profiles 按 account_id 索引。任何一跳缺失都退化成 NULL，
-- 不让「作者注销了」把整条队列打成 500。
SELECT d.id,
       d.slug,
       d.title,
       d.description,
       d.created_at,
       COALESCE(NULLIF(p.display_name, ''), p.username, '')::text AS author_name,
       COALESCE(r.rev_no, 0)::int AS current_rev_no
FROM demos d
LEFT JOIN users u ON u.id = d.author_id
LEFT JOIN accounts a ON a.sub = u.sub
LEFT JOIN profiles p ON p.account_id = a.id
LEFT JOIN demo_revisions r ON r.id = d.current_revision_id
WHERE d.moderation_state = 'pending'
  AND d.deleted_at IS NULL
  AND d.erased_at IS NULL
ORDER BY d.created_at ASC, d.id ASC
LIMIT $1;

-- name: GetDemoForReview :one
-- 裁决前先取一次：用来区分「不存在」（404）与「已经裁决过」（409）。
-- 这两者必须区分——前者是调用方地址写错，后者是操作顺序问题。
SELECT id, slug, moderation_state, deleted_at, erased_at
FROM demos
WHERE slug = $1;

-- name: ReviewDemo :one
-- 条件写进 WHERE 而不是先查后改：并发两次裁决时，只有一条能把行从 pending 改走，
-- 另一条拿到 0 行。先查后改在这个窗口里两条都会通过，然后第二次覆盖第一次的结果。
UPDATE demos
SET moderation_state = $2,
    updated_at       = now()
WHERE slug = $1
  AND moderation_state = 'pending'
  AND deleted_at IS NULL
  AND erased_at IS NULL
RETURNING id, slug, moderation_state;

-- name: InsertDemoTimeline :exec
-- 写给人看的经过（Demo 域 §3.11）。它不承担审计职责——审计在 audit_log。
INSERT INTO demo_timeline (demo_id, kind, message, actor_id)
VALUES ($1, $2, NULLIF($3::text, ''), $4);

-- name: InsertAuditLog :exec
-- 治理域 §5.2：actor 三列必须由服务端从会话取，调用方不能自报。
-- before / after 只存关键字段的快照，不存整行（内容不进展）。
INSERT INTO audit_log (actor_type, actor_id, actor_role, action,
                       entity_type, entity_id, before, after, reason)
VALUES ('user', $1, $2, 'review',
        'demo', $3, $4, $5, $6);
