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



-- name: ApproveClassificationProposal :one
-- new_classification 的执行器：在**一条语句**里完成「建分类 + 提案置 approved」。
--
-- 为什么是一条语句而不是 Go 里 begin/commit：handler 没有 pool；而这条 CTE 天然原子——
-- target 先 FOR UPDATE 锁住 pending 提案，ensured 只在锁到行时插分类，
-- updated 只在同一行仍是 pending 时改状态。任何一步不满足，整条语句都不产生副作用。
--
-- 分类已存在（同 dimension+slug）：ON CONFLICT 做无意义 UPDATE 拿回 id，
-- 对应 ENT-021 的「目标已存在 → 复用」，不是 500、也不覆盖已有 label。
WITH target AS (
    SELECT p.id AS proposal_id
    FROM proposals p
    WHERE p.id = @id AND p.status = 'pending' AND p.erased_at IS NULL
    FOR UPDATE
), ensured AS (
    INSERT INTO classifications (dimension, slug, label, description)
    SELECT @dimension, @slug, @label, @description FROM target
    ON CONFLICT (dimension, slug) DO UPDATE
    SET updated_at = classifications.updated_at
    RETURNING classifications.id
), updated AS (
    UPDATE proposals p
    SET status = 'approved',
        result_kind = 'classification',
        result_id = (SELECT e.id FROM ensured e),
        reviewed_by = sqlc.narg('reviewed_by'),
        reviewed_at = now(),
        updated_at = now()
    WHERE p.id = @id AND p.status = 'pending'
    RETURNING p.id
)
SELECT COALESCE((SELECT e.id FROM ensured e), 0)::bigint AS classification_id,
       EXISTS (SELECT 1 FROM updated) AS approved;


-- name: ApproveNewSkillProposal :one
-- new_skill 的执行器：与 new_classification 同一骨架。
-- slug 已存在时复用（ENT-021），不覆盖已有 name / url / description。
WITH target AS (
    SELECT p.id AS proposal_id
    FROM proposals p
    WHERE p.id = @id AND p.status = 'pending' AND p.erased_at IS NULL
    FOR UPDATE
), ensured AS (
    INSERT INTO skills (slug, name, description, url, spec)
    SELECT @slug, @name, @description, sqlc.narg('url')::text, @spec FROM target
    ON CONFLICT (slug) DO UPDATE
    SET updated_at = skills.updated_at
    RETURNING skills.id
), updated AS (
    UPDATE proposals p
    SET status = 'approved',
        result_kind = 'skill',
        result_id = (SELECT e.id FROM ensured e),
        reviewed_by = sqlc.narg('reviewed_by'),
        reviewed_at = now(),
        updated_at = now()
    WHERE p.id = @id AND p.status = 'pending'
    RETURNING p.id
)
SELECT COALESCE((SELECT e.id FROM ensured e), 0)::bigint AS skill_id,
       EXISTS (SELECT 1 FROM updated) AS approved;


-- name: ApproveNewTaskProposal :one
-- new_task 的执行器：锁 pending 提案 → 已有同名题（title_key）则复用 → 否则建题
-- → 提案置 approved + result_kind=task。
--
-- 标题键由应用层算好传进来（domain/entity.TitleKey）；这里只负责原子写入。
-- 暂不处理 payload 里的 subject demo（attach_demos 需要 prompt 归类，下一轮）。
WITH target AS (
    SELECT p.id AS proposal_id
    FROM proposals p
    WHERE p.id = @id AND p.status = 'pending' AND p.erased_at IS NULL
    FOR UPDATE
), existing AS (
    SELECT t.id
    FROM tasks t
    CROSS JOIN target
    WHERE t.title_key = @title_key
      AND t.status = 'active'
      AND t.deleted_at IS NULL
), ensured AS (
    INSERT INTO tasks (slug, title, title_key, title_norm, brief, category, created_by)
    SELECT @slug, @title, @title_key, @title_norm, @brief,
           sqlc.narg('category'), sqlc.narg('created_by')
    FROM target
    WHERE NOT EXISTS (SELECT 1 FROM existing)
    RETURNING tasks.id
), resolved AS (
    SELECT e.id FROM existing e
    UNION ALL
    SELECT n.id FROM ensured n
    LIMIT 1
), updated AS (
    UPDATE proposals p
    SET status = 'approved',
        result_kind = 'task',
        result_id = (SELECT r.id FROM resolved r),
        reviewed_by = sqlc.narg('reviewed_by'),
        reviewed_at = now(),
        updated_at = now()
    WHERE p.id = @id AND p.status = 'pending'
    RETURNING p.id
)
SELECT COALESCE((SELECT r.id FROM resolved r), 0)::bigint AS task_id,
       EXISTS (SELECT 1 FROM updated) AS approved;
