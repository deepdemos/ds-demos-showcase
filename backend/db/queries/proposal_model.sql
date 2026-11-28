-- 批准 new_model 收录提案所需的事务内查询（#593）。
--
-- 为什么拆独立文件而不塞进 proposals.sql / models.sql：避免跨票修改热点查询文件产生合并冲突，
-- sqlc 自动扫描 db/queries 目录。

-- name: LockPendingProposalForModel :one
-- 在事务内锁住待审提案，防止并发两次批准同时建模型或绕过状态检查。
SELECT id, kind, status, payload
FROM proposals
WHERE id = @id AND status = 'pending' AND erased_at IS NULL
FOR UPDATE;

-- name: FindOfferingOwner :one
-- 查一条上游三元组是否已被某个本地模型认领，并带出认领方的状态与合并归宿，
-- 便于在冲突（409）时直接向管理员说明归属（模型域 §6.7.4）。
SELECT m.id,
       m.slug,
       m.name,
       m.status,
       COALESCE(dst.slug, '')::text AS merged_into_slug
FROM model_offerings r
JOIN models m ON m.id = r.model_id
LEFT JOIN models dst ON dst.id = m.merged_into_id
WHERE r.authority = @authority
  AND r.provider_id = @provider_id
  AND r.external_id = @external_id;

-- name: InsertModelOfferingStrict :exec
-- 提案批准当场认领上游三元组：刻意不写 ON CONFLICT DO UPDATE，
-- 并发抢占同一三元组时让唯一约束抛出 23505 回滚整笔事务，绝不悄悄夺走别人的认领。
INSERT INTO model_offerings (model_id, authority, provider_id, provider_name, external_id, canonical_id, is_official)
VALUES (@model_id, @authority, @provider_id, @provider_name, @external_id, sqlc.narg('canonical_id'), @is_official);

-- name: MarkProposalApproved :one
-- 同一事务内将已锁住的 pending 提案置为 approved，并记录产出的实体类型与编号。
UPDATE proposals
SET status = 'approved',
    result_kind = @result_kind,
    result_id = @result_id,
    reviewed_by = sqlc.narg('reviewed_by'),
    reviewed_at = now(),
    updated_at = now()
WHERE id = @id AND status = 'pending' AND erased_at IS NULL
RETURNING id;
