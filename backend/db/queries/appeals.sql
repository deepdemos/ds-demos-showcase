-- 作品申诉（#463）。一表一语义：不塞进 proposals。

-- name: GetDemoAppealContext :one
-- 认人 + 认状态：作者账号、当前生命周期；软删 / 擦除的 slug 直接当不存在。
SELECT d.id AS demo_id,
       d.slug,
       d.lifecycle_state,
       COALESCE(ac.id, 0)::bigint AS author_account_id
FROM demos d
LEFT JOIN users u ON u.id = d.author_id
LEFT JOIN accounts ac ON ac.sub = u.sub
WHERE d.slug = @slug
  AND d.deleted_at IS NULL
  AND d.erased_at IS NULL;

-- name: GetLatestDemoAppeal :one
SELECT id, demo_id, reason, status, reviewed_at, decision_reason, created_at
FROM demo_appeals
WHERE demo_id = @demo_id
ORDER BY created_at DESC, id DESC
LIMIT 1;

-- name: GetPendingAppealBySlug :one
-- 写操作闸用：这个作品有没有在途申诉。
SELECT a.id
FROM demo_appeals a
JOIN demos d ON d.id = a.demo_id
WHERE d.slug = @slug AND a.status = 'pending'
LIMIT 1;

-- name: CreateDemoAppeal :one
-- 条件写进 SELECT：只有「作者 + removed + 无 pending」才插得进去；
-- 并发时部分唯一索引兜底，命中 0 行由 handler 区分 409。
INSERT INTO demo_appeals (demo_id, appellant_account_id, reason)
SELECT d.id, @account_id, @reason
FROM demos d
WHERE d.slug = @slug
  AND d.lifecycle_state = 'removed'
  AND d.deleted_at IS NULL
  AND d.erased_at IS NULL
  AND EXISTS (
      SELECT 1
      FROM users u
      JOIN accounts ac ON ac.sub = u.sub
      WHERE u.id = d.author_id AND ac.id = @account_id
  )
ON CONFLICT (demo_id) WHERE status = 'pending' DO NOTHING
RETURNING id, demo_id, reason, status, reviewed_at, decision_reason, created_at;
