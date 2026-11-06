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
--
-- 多带两列是给「通过时切换对外版本」用的（迁移 0014）：裁决的对象是**当前版本**，
-- 通过之后对外服务的版本才跟着换；驳回则保持原样。
SELECT id, slug, moderation_state, deleted_at, erased_at,
       current_revision_id, published_revision_id
FROM demos
WHERE slug = $1;

-- name: ReviewDemo :one
-- 条件写进 WHERE 而不是先查后改：并发两次裁决时，只有一条能把行从 pending 改走，
-- 另一条拿到 0 行。先查后改在这个窗口里两条都会通过，然后第二次覆盖第一次的结果。
UPDATE demos
SET moderation_state = $2,
    updated_at       = GREATEST(now(), created_at)
WHERE slug = $1
  AND moderation_state = 'pending'
  AND deleted_at IS NULL
  AND erased_at IS NULL
RETURNING id, slug, moderation_state;

-- name: SetPublishedRevision :exec
-- 审核通过时把对外版本切到当前版本（迁移 0014 的版本分离）。
-- 幂等：重复通过不会改到别的版本上（WHERE 里带 slug 而不是 id，是为了让调用方
-- 只用 slug 就能定位；同一事务里刚刚 ReviewDemo 过，行一定存在）。
-- 顺带把 demos.title / description 改成这一版的内容：列表查的是 demos 上的这两列
-- （`ListPublishedDemos` 读 d.title），而版本的标题存在 demo_revisions 上。
-- 不同步的话，「改了标题 → 审过了 → 列表里还是老标题」，而详情页读的是新的。
-- 只在审核通过那一刻同步：pending 期间列表必须继续显示老标题（对外那一版）。
UPDATE demos d
SET published_revision_id = d.current_revision_id,
    title                 = COALESCE(r.title, d.title),
    description           = COALESCE(r.description, d.description),
    updated_at            = GREATEST(now(), d.created_at)
FROM demo_revisions r
WHERE d.slug = $1
  AND d.current_revision_id IS NOT NULL
  AND r.id = d.current_revision_id;

-- name: InsertDemoTimeline :exec
-- 写给人看的经过（Demo 域 §3.11）。它不承担审计职责——审计在 audit_log。
INSERT INTO demo_timeline (demo_id, kind, message, actor_id)
VALUES ($1, $2, NULLIF($3::text, ''), $4);

-- name: InsertAuditLog :exec
-- 治理域 §5.2：actor 三列必须由服务端从会话取，调用方不能自报。
-- before / after 只存关键字段的快照，不存整行（内容不进展）。
INSERT INTO audit_log (actor_type, actor_id, actor_role, action,
                       entity_type, entity_id, before, after, reason)
VALUES ('user', sqlc.arg(actor_id), sqlc.arg(actor_role), sqlc.arg(action),
        'demo', sqlc.arg(entity_id), sqlc.arg(before), sqlc.arg(after), sqlc.arg(reason));

-- ---------- 发布（治理域 §4.3 的 demo.publish）----------

-- name: GetDemoForPublish :one
-- 发布前的状态检查。与 GetDemoForReview 分开：这里要 lifecycle_state 与
-- moderation_state 两个一起判，而审核只看后者。
SELECT d.id, d.slug, d.lifecycle_state, d.moderation_state, d.deleted_at, d.erased_at
FROM demos d
WHERE d.slug = $1;

-- name: PublishDemo :execrows
-- 同一条语句里判状态并跃迁：并发两次发布只有一次能改走。
-- 允许的来源状态写在 WHERE 里，`published` 不在其中 —— 重复发布返回 0 行，
-- 调用方据此回 409，而不是让「已经是发布态」和「刚发布成功」都返回 200。
UPDATE demos
SET lifecycle_state       = 'published',
    -- 第一次发布时把对外版本指向当前版本；已经有了就不动（复审期间发布的场景：
    -- 对外内容仍是上一版，发布动作不该顺手把未审的新版本推上线）。
    published_revision_id = COALESCE(published_revision_id, current_revision_id),
    updated_at            = GREATEST(now(), created_at)
WHERE slug = $1
  AND lifecycle_state IN ('draft', 'archived')
  AND moderation_state = 'approved'
  AND deleted_at IS NULL
  AND erased_at IS NULL;

-- name: UpsertDemoChannel :exec
-- 同一作品同一渠道重复发布时覆盖可见性，不新增行（uq_channel 保证唯一）。
INSERT INTO demo_channels (demo_id, channel, visibility, lang, published_at, curated_by)
VALUES ($1, $2, $3, $4, now(), $5)
ON CONFLICT (demo_id, channel) DO UPDATE
SET visibility = EXCLUDED.visibility,
    lang = EXCLUDED.lang,
    published_at = COALESCE(demo_channels.published_at, EXCLUDED.published_at),
    curated_by = EXCLUDED.curated_by,
    -- 与 demos 的时间字段同一口径：时钟回拨时现在这一刻可能早于这行的 created_at，
    -- demo_channels 上有 ck_channel_time，写回退后的值会把发布打成 503。
    updated_at = GREATEST(now(), demo_channels.created_at);
