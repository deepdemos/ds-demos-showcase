-- 模型维度的查询。
--
-- 为什么单独一个文件：`db/queries/` 原来只有 authz/blob/demos/identity/review/settings/
-- verification 七个——模型域有 15 张表，却一条 SQL 都没有。详情页要展示「这个作品用了
-- 哪些模型」，靠的就是这里的四个查询。

-- name: ListActiveModels :many
-- 下拉与筛选用的主数据：只出 `active` 的，且按名字排序（选单里乱序等于让人找不到）。
-- `unverified` / `candidate` 不上选单——它们还没有被确认过，不该被作者当事实声明。
SELECT id, slug, name, COALESCE(vendor, '')::text AS vendor
FROM models
WHERE status = 'active'
ORDER BY name, id;

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
-- 排序按名字，让同一个作品两次打开的顺序一致。
SELECT mo.slug, mo.name, COALESCE(mo.vendor, '')::text AS vendor
FROM demo_revision_models rm
JOIN models mo ON mo.id = rm.model_id
WHERE rm.revision_id = $1
ORDER BY mo.name, mo.id;
