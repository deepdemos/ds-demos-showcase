-- 随投稿 / 编辑提交的模型收录提案（#590）。
--
-- 单独建文件避免与 proposals.sql 冲突；sqlc 自动扫描 db/queries 目录。

-- name: InsertModelProposal :one
-- 写入一条 new_model 待审提案。
-- 去重由 uq_proposals_pending_dedupe (kind, dedupe_key) WHERE status='pending' 兜底：
-- 同一作品重复提交同名模型时 ON CONFLICT DO NOTHING 返回 0 行（pgx.ErrNoRows），
-- 既不造出重复待审提案，也不阻断作品提交。
INSERT INTO proposals (kind, payload, created_by, source, dedupe_key)
VALUES ('new_model', @payload::jsonb, @created_by, 'user', @dedupe_key)
ON CONFLICT (kind, dedupe_key) WHERE status = 'pending' AND dedupe_key IS NOT NULL
DO NOTHING
RETURNING id;

-- name: InsertProposalSubjectDemoRef :exec
-- 用 proposal_refs(role='subject') 把提案挂到来源作品。
-- 与 proposals 行同事务写入，保证来源可追溯，且作品物理删除时引用级联清理。
INSERT INTO proposal_refs (proposal_id, role, demo_id)
VALUES (@proposal_id, 'subject', @demo_id)
ON CONFLICT (proposal_id, role) DO NOTHING;

-- name: DeleteProposalsForDemoCleanup :exec
-- 投稿上传失败触发补偿删作品前，顺手清掉本次刚建的 subject 提案，
-- 避免留下没有对应作品的孤儿待审提案（FK 只从 proposal_refs 级联到 demos，不会反删 proposals）。
DELETE FROM proposals
WHERE id IN (
    SELECT proposal_id
    FROM proposal_refs
    WHERE demo_id = @demo_id AND role = 'subject'
);
