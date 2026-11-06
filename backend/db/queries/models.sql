-- 模型维度的查询。
--
-- 为什么单独一个文件：`db/queries/` 原来只有 authz/blob/demos/identity/review/settings/
-- verification 七个——模型域有 15 张表，却一条 SQL 都没有。详情页要展示「这个作品用了
-- 哪些模型」，靠的就是这里的四个查询。

-- name: ListModels :many
-- 模型目录：**全部状态**，可以按 status 筛，也可以不筛。
--
-- 早先这里写死 `WHERE status = 'active'`，于是契约里的 `status` 字段永远是同一个值——
-- 那等于没给。把筛选交给调用方（`?status=candidate`），字段才有意义。
--
-- `source` 是派生的，不是列：`models` 表没有这一列，「这个模型是不是被上游目录认领过」
-- 本来就是 `model_external_refs` 的事实。多加一列等于给同一件事两个出处。
--
-- `merged_into_slug` 而不是 `merged_into_id`：合并归宿要用 slug 表达（引用一律 slug），
-- 数字 id 是实现细节。
SELECT m.id,
       m.slug,
       m.name,
       COALESCE(m.vendor, '')::text AS vendor,
       m.status,
       m.resolution,
       COALESCE(t.slug, '')::text AS merged_into_slug,
       CASE
           WHEN EXISTS (SELECT 1 FROM model_external_refs r WHERE r.model_id = m.id) THEN 'models.dev'
           ELSE 'manual'
       END::text AS source,
       m.description,
       m.created_at,
       m.updated_at
FROM models m
LEFT JOIN models t ON t.id = m.merged_into_id
WHERE sqlc.narg('status')::text IS NULL OR m.status = sqlc.narg('status')::text
ORDER BY m.name, m.id;

-- name: ListModelAliases :many
-- 全部别名，按 model_id 分组用。
--
-- 一次取全量而不是「每个模型一次查询」：别名表是解析入口，行数比 models 少一个量级
-- （同一个模型的五六种写法），单独一条 SQL 比 N+1 更省。
SELECT model_id, alias
FROM model_aliases
ORDER BY model_id, alias;

-- name: ResolveModelSlugs :many
-- 把前端传来的 slug 换成 id，并**顺手做存在性与状态校验**：
-- 传上来的 slug 里只要有一个不是 active，这里返回的行数就比输入的少，
-- 调用方据此报 400（而不是悄悄丢掉一个声明）。
SELECT id, slug
FROM models
WHERE slug = ANY(sqlc.arg(slugs)::text[])
  AND status = 'active'
ORDER BY id;

-- name: InsertRevisionModel :exec
-- `ON CONFLICT DO NOTHING`：同一个模型重复提交（重复字段、去重前的脏数据）不该让整次投稿失败。
-- 主键 (revision_id, model_id) 保证同一模型经不同 provider 只算一条。
INSERT INTO demo_revision_models (revision_id, model_id, provider_id, serving_note, source)
VALUES (sqlc.arg(revision_id), sqlc.arg(model_id), NULL, '', 'api')
ON CONFLICT ON CONSTRAINT demo_revision_models_pkey DO NOTHING;

-- name: CopyRevisionModels :exec
-- 回滚时把源版本的模型声明整表复制到新版本。
--
-- 为什么必须复制：回滚追加的是**新版本**（`source = 'rollback'`），而模型声明挂在
-- revision 上。不复制的话，一次回滚会把「这个作品用了哪些模型」抹掉——
-- 内容回到旧版、声明却消失了，这与「回滚到那一版」的语义不符。
INSERT INTO demo_revision_models (revision_id, model_id, provider_id, serving_note, source)
SELECT sqlc.arg(target_revision_id), m.model_id, m.provider_id, m.serving_note, m.source
FROM demo_revision_models m
WHERE m.revision_id = sqlc.arg(source_revision_id)
ON CONFLICT ON CONSTRAINT demo_revision_models_pkey DO NOTHING;

-- name: ListRevisionModels :many
-- 详情页用：某一版的模型声明（作者视图看当前版本、访客看对外版本）。
--
-- 字段面与 ListModels **逐字一致**：详情里的模型对象与目录里的模型对象是同一个形状，
-- 否则前端要为两处写两套渲染（同一份数据在两张页面上长得不一样，是最容易被忽略的
-- 不一致）。排序按名字，让同一个作品两次打开的顺序一致。
SELECT mo.id,
       mo.slug,
       mo.name,
       COALESCE(mo.vendor, '')::text AS vendor,
       mo.status,
       mo.resolution,
       COALESCE(t.slug, '')::text AS merged_into_slug,
       CASE
           WHEN EXISTS (SELECT 1 FROM model_external_refs r WHERE r.model_id = mo.id) THEN 'models.dev'
           ELSE 'manual'
       END::text AS source,
       mo.description,
       mo.created_at,
       mo.updated_at
FROM demo_revision_models rm
JOIN models mo ON mo.id = rm.model_id
LEFT JOIN models t ON t.id = mo.merged_into_id
WHERE rm.revision_id = $1
ORDER BY mo.name, mo.id;

-- name: ListRevisionModelAliases :many
-- 详情里那些模型各自的别名（与 ListRevisionModels 同一批 model_id）。
SELECT ma.model_id, ma.alias
FROM model_aliases ma
WHERE ma.model_id IN (
    SELECT rm.model_id FROM demo_revision_models rm WHERE rm.revision_id = $1
)
ORDER BY ma.model_id, ma.alias;
