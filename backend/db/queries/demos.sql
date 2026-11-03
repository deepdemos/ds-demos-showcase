-- 作品列表与详情。
--
-- 「公开可见」的判定口径（两处必须一致，改一处就要改另一处）：
--   lifecycle_state = 'published'   —— 已发布，见标准对象 §ST-2 的可见性档位
--   deleted_at IS NULL              —— 没被删除
--   erased_at IS NULL               —— 没被擦除（擦除是逻辑封存，行还在但对外不存在）
-- 已经归档（archived）或移除（removed）的作品**不在列表里**，但详情页仍可访问（墓碑），
-- 所以这里查询条件与 GetDemoBySlug 故意不同——这不是漏写。

-- name: ListPublishedDemos :many
SELECT id, slug, title, cover_url, created_at
FROM demos
WHERE lifecycle_state = 'published'
  AND deleted_at IS NULL
  AND erased_at IS NULL
ORDER BY created_at DESC, id DESC
LIMIT $1 OFFSET $2;

-- name: CountPublishedDemos :one
SELECT count(*)
FROM demos
WHERE lifecycle_state = 'published'
  AND deleted_at IS NULL
  AND erased_at IS NULL;

-- name: GetDemoBySlug :one
-- 不加可见性过滤：归档/移除的作品要能取出来渲染墓碑页。
-- 「要不要给 404」由调用方按 lifecycle_state / erased_at 判定。
SELECT id, slug, title, description, cover_url,
       lifecycle_state, created_at, updated_at, deleted_at, erased_at
FROM demos
WHERE slug = $1;

-- ===== 投稿写入（Demo 域）=====
-- 这里只做「新作品第一次投稿」需要的三条写。版本追加、审核状态变更、下架等
-- 都是后续端点的事，不在这里提前写。

-- name: CreateDemo :one
-- 刚提交的作品一律 moderation_state='pending' + lifecycle_state='draft'：
-- 待审核、且对外不可见。两个状态是**正交**的两件事，不要合并成一个字段。
INSERT INTO demos (slug, title, description, author_id, moderation_state, lifecycle_state)
VALUES ($1, $2, $3, $4, 'pending', 'draft')
RETURNING id, slug, created_at;

-- name: CreateRevision :one
-- rev_no 写死 1：这是新作品的第一版。追加版本是 updateDemo 的事。
INSERT INTO demo_revisions (
    demo_id, rev_no, title, description, demo_type,
    entry_path, single_file, manifest_hash, file_count, total_bytes, source, created_by
) VALUES ($1, 1, $2, $3, $4, $5, $6, $7, $8, $9, 'upload', $10)
RETURNING id, rev_no;

-- name: SetCurrentRevision :exec
UPDATE demos SET current_revision_id = $2, updated_at = now() WHERE id = $1;

-- name: DeleteRevisionsForCleanup :exec
-- 仅用于「投稿中途失败」的补偿：这些版本从未对外可见。
--
-- 为什么要显式删：`demo_revisions.demo_id` 目前**没有外键**（`db/schema/80-late-fk.sql`
-- 的注释解释了 demos 与 demo_revisions 互为先后、只能集中添加，但这两条边至今没加），
-- 所以删 demos 那一行不会带走版本行。这里按 demo_id 整批删，不依赖级联。
DELETE FROM demo_revisions WHERE demo_id = $1;

-- name: DeleteDemoForCleanup :exec
-- 仅用于「投稿中途失败」的补偿：这一行是刚刚在同一请求里建的、从未对外可见。
-- **正常删除不走这里**——正常删除是生命周期域的软删 + 墓碑。
DELETE FROM demos WHERE id = $1;
