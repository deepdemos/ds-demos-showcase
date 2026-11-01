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
