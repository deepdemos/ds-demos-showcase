-- 聚合收件箱（#433 / 治理域 D4）。
--
-- 跨 kind 的待办汇总：demo 两段队列（待审 / 已通过待发布）走 demo 域已有的查询，
-- 这里只补计数与提案列表。提案的裁决与执行下一轮接入，先让收件箱看得到。

-- name: CountInboxDemoPending :one
SELECT count(*)::bigint AS total
FROM demos
WHERE moderation_state = 'pending'
  AND deleted_at IS NULL
  AND erased_at IS NULL;

-- name: CountInboxApprovedUnpublished :one
SELECT count(*)::bigint AS total
FROM demos
WHERE moderation_state = 'approved'
  AND lifecycle_state = 'draft'
  AND deleted_at IS NULL
  AND erased_at IS NULL;

-- name: CountInboxPendingProposals :one
SELECT count(*)::bigint AS total
FROM proposals
WHERE status = 'pending' AND erased_at IS NULL;

-- name: ListInboxPendingProposals :many
-- 只给收件箱摘要：完整 payload 留给提案详情页（下一轮）。
SELECT id, kind, source, payload, created_at
FROM proposals
WHERE status = 'pending' AND erased_at IS NULL
ORDER BY created_at ASC, id ASC
LIMIT @row_limit;
