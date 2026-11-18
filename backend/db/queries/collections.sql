-- 收藏域（#438 / COL-001 – COL-014）。
--
-- 上限不写死：collection.max_per_account / collection.max_items 从设置域读，
-- 由调用方传进来（本文件只收 max_* 参数）。默认夹惰性创建，不写触发器。

-- name: EnsureDefaultCollection :one
-- 默认夹每人一个：先插，冲突时做一次无意义 UPDATE 拿回 id。
-- 不用「先查再插」：两个人同时第一次收藏时，后者会撞唯一索引。
INSERT INTO collections (account_id, name, is_default)
VALUES (@account_id, '默认收藏', true)
ON CONFLICT (account_id) WHERE is_default DO UPDATE
SET name = collections.name
RETURNING id;

-- name: ListCollectionsByAccount :many
-- 我的夹列表：默认夹排最前，其余按最近更新。
SELECT c.id, c.name, c.description, c.is_default, c.visibility,
       c.created_at, c.updated_at,
       (SELECT count(*) FROM collection_items ci WHERE ci.collection_id = c.id)::bigint AS item_count
FROM collections c
WHERE c.account_id = @account_id
ORDER BY c.is_default DESC, c.updated_at DESC, c.id DESC;

-- name: GetCollectionByID :one
SELECT id, account_id, name, description, is_default, visibility, created_at, updated_at
FROM collections
WHERE id = @id;

-- name: CountCustomCollections :one
SELECT count(*)::bigint AS total
FROM collections
WHERE account_id = @account_id AND NOT is_default;

-- name: CreateCollection :one
-- 上限在 SQL 里判：应用层先数再插会在并发下多建一个。
-- 唯一名冲突由 uq_collections_name 抛 23505，handler 映射 422。
INSERT INTO collections (account_id, name, description)
SELECT @account_id, @name, @description
WHERE (SELECT count(*) FROM collections AS c2
       WHERE c2.account_id = @account_id AND NOT c2.is_default) < @max_collections
RETURNING id, name, description, is_default, visibility, created_at, updated_at;

-- name: UpdateCollection :one
-- 默认夹不可改：WHERE 里带 NOT is_default，命中 0 行由 handler 回 422。
-- 三个字段都可选：NULL = 不动。
UPDATE collections
SET name        = COALESCE(sqlc.narg(name)::text, name),
    description = COALESCE(sqlc.narg(description)::text, description),
    visibility  = COALESCE(sqlc.narg(visibility)::text, visibility),
    updated_at  = now()
WHERE id = @id
  AND account_id = @account_id
  AND NOT is_default
RETURNING id, name, description, is_default, visibility, created_at, updated_at;

-- name: DeleteCollection :execrows
DELETE FROM collections
WHERE id = @id AND account_id = @account_id AND NOT is_default;

-- name: GetPublicDemoIDBySlug :one
-- 只允许收藏当前公开作品：草稿 / 下架 / 已删的 slug 一律「加不进去」。
SELECT d.id
FROM demos d
JOIN demo_public_demos p ON p.demo_id = d.id
WHERE d.slug = @slug;

-- name: AddCollectionItem :execrows
-- 单夹上限同 SQL 判；重复加同一件作品是 ON CONFLICT DO NOTHING（COL-006）。
INSERT INTO collection_items (collection_id, demo_id)
SELECT @collection_id, @demo_id
WHERE (SELECT count(*) FROM collection_items AS ci2
       WHERE ci2.collection_id = @collection_id) < @max_items
ON CONFLICT DO NOTHING;

-- name: RemoveCollectionItem :execrows
-- 幂等：本来就不在夹里也是 0 行，handler 照 204 返回。
DELETE FROM collection_items AS ci
USING collections c
WHERE ci.collection_id = c.id
  AND c.id = @collection_id
  AND c.account_id = @account_id
  AND ci.demo_id IN (SELECT id FROM demos WHERE slug = @slug);

-- name: GetCollectionItemCount :one
SELECT count(*)::bigint AS total FROM collection_items WHERE collection_id = @collection_id;

-- name: ListCollectionItems :many
-- accessible 用 demo_public_demos 判：不在视图里的条目仍保留（COL-014 墓碑），
-- 但前端要显示「该作品已不可访问」并不给跳转。
SELECT d.slug,
       d.title,
       d.cover_url,
       ci.added_at,
       EXISTS (SELECT 1 FROM demo_public_demos p WHERE p.demo_id = d.id) AS accessible
FROM collection_items ci
JOIN demos d ON d.id = ci.demo_id
WHERE ci.collection_id = @collection_id
ORDER BY ci.added_at DESC, d.id DESC;

-- name: CollectionStatusForSlug :many
-- 详情页按钮状态：这件作品在默认夹吗？在哪些自定义夹？
SELECT c.id, c.is_default
FROM collection_items ci
JOIN collections c ON c.id = ci.collection_id
JOIN demos d ON d.id = ci.demo_id
WHERE c.account_id = @account_id
  AND d.slug = @slug
ORDER BY c.is_default DESC, c.id;
