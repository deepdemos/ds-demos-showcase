-- 提案审核（#433 / ENT-016 – ENT-024）。
--
-- 这一轮只做「看得清 + 能驳回」：驳回不写任何正式业务表（ENT-023），风险最低；
-- 通过的 execute(kind) 矩阵下一轮按 kind 接入，未接入前明确 422，不假装成功。

-- name: ListProposalsForAdmin :many
-- status / kind / source 空串表示不过滤。默认由 handler 传 'pending'。
SELECT id, kind, status, source, payload, created_at, updated_at
FROM proposals
WHERE erased_at IS NULL
  AND (sqlc.arg(status)::text = '' OR status = sqlc.arg(status))
  AND (sqlc.arg(kind)::text = '' OR kind = sqlc.arg(kind))
  AND (sqlc.arg(source)::text = '' OR source = sqlc.arg(source))
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(row_limit) OFFSET sqlc.arg(row_offset);

-- name: CountProposalsForAdmin :one
SELECT count(*)::bigint AS total
FROM proposals
WHERE erased_at IS NULL
  AND (sqlc.arg(status)::text = '' OR status = sqlc.arg(status))
  AND (sqlc.arg(kind)::text = '' OR kind = sqlc.arg(kind))
  AND (sqlc.arg(source)::text = '' OR source = sqlc.arg(source));

-- name: GetProposalByID :one
SELECT id, kind, status, payload, source, confidence,
       created_by, created_at,
       reviewed_by, reviewed_at, reject_reason,
       result_kind, result_id, updated_at
FROM proposals
WHERE id = @id AND erased_at IS NULL;

-- name: RejectProposal :one
-- 条件写进 WHERE：并发两次驳回只有一次命中；命中 0 行时调用方再区分 404 / 409。
UPDATE proposals
SET status = 'rejected',
    reviewed_by = @reviewed_by,
    reviewed_at = now(),
    reject_reason = @reject_reason,
    updated_at = now()
WHERE id = @id AND status = 'pending' AND erased_at IS NULL
RETURNING id, status;

