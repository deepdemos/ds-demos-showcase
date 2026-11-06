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

-- name: ResolveModelRefs :many
-- 把作者填的「模型指称」解析成模型 id。指称可以是 slug、展示名或别名，**大小写不敏感**。
--
-- 为什么一条 SQL 里带三种匹配：作者在投稿框里手打的是「DeepSeek V4 Flash」这种人话，
-- 不是 slug；只认 slug 等于要求他先去查目录。三种形态在库里本来就是同一件事的三种写法
-- （`models.slug` / `models.name` / `model_aliases.alias`），解析放在一处，别让每个调用点
-- 各写一套优先级。
--
-- `rank` 决定同名不同形态时谁优先：slug(0) > alias(1) > name(2)。同 rank 再按 id 取小的，
-- 保证结果稳定——否则同一份输入两次解析可能命中不同的行，作者会看到模型莫名换掉。
--
-- 已合并的模型（`status='deprecated'` 且 `merged_into_id` 非空）沿指针走一跳，返回归宿 id：
-- 作者手里可能还留着旧名字，报错「模型不存在」既不准确也没帮助。只走一跳——多级合并链
-- 在库里没出现过，真出现时再按需展开。
WITH input AS (
    SELECT DISTINCT lower(btrim(r))::text AS ref
    FROM unnest(sqlc.arg(refs)::text[]) AS r
    WHERE btrim(r) <> ''
),
cand AS (
    SELECT i.ref,
           COALESCE(t.id, m.id) AS model_id,
           CASE
               WHEN lower(m.slug) = i.ref THEN 0
               WHEN EXISTS (
                   SELECT 1 FROM model_aliases a
                   WHERE a.model_id = m.id AND lower(a.alias) = i.ref
               ) THEN 1
               ELSE 2
           END AS rank
    FROM input i
    JOIN models m
      ON lower(m.slug) = i.ref
      OR lower(m.name) = i.ref
      OR EXISTS (
          SELECT 1 FROM model_aliases a
          WHERE a.model_id = m.id AND lower(a.alias) = i.ref
      )
    LEFT JOIN models t
      ON t.id = m.merged_into_id AND m.status = 'deprecated'
)
SELECT DISTINCT ON (ref) ref::text AS ref, model_id
FROM cand
ORDER BY ref, rank, model_id;

-- name: CreateCandidateModel :one
-- 作者填了一个库里没有的名字：**立刻建实体**，状态 `candidate`。
--
-- 为什么不让投稿失败：AI 圈的灰测模型等不起人工审核（旧站的 142 个模型写法里有大量
-- 上游没收录的名字）。先落 `candidate` 让作品能提交，管理员事后归并——归并是按 id 改指针，
-- 作品不用动。
--
-- `ON CONFLICT DO NOTHING` 不带目标：`slug` 与 `name` 两个唯一约束都要兜住并发。
-- 冲突时不返回行，调用方据此回查现有行（而不是把它当失败）。
INSERT INTO models (slug, name, vendor, status, resolution, description)
VALUES (
    sqlc.arg(slug),
    sqlc.arg(name),
    sqlc.narg(vendor),
    'candidate',
    'exact',
    ''
)
ON CONFLICT DO NOTHING
RETURNING id;

-- name: FindModelByName :one
-- 建档撞唯一约束后回查：名字是作者给的，slug 才是机器生成的，按名字找人话更准。
SELECT id
FROM models
WHERE lower(name) = lower(sqlc.arg(name))
LIMIT 1;

-- name: InsertModelAlias :exec
-- 把作者这次填的原文记成别名，下次同样的写法直接命中，不用再建一遍。
--
-- `alias` 是单列主键（全库唯一）：同一个写法不允许同时指两个模型，否则解析结果取决于
-- 查询顺序——那是比「解析不到」更糟的状态。
INSERT INTO model_aliases (alias, model_id)
VALUES (sqlc.arg(alias), sqlc.arg(model_id))
ON CONFLICT (alias) DO NOTHING;

-- name: ModelSlugTaken :one
-- slug 是公开 URL 的一部分、创建后不可变，生成时先查一遍再落库。
SELECT EXISTS (SELECT 1 FROM models WHERE slug = $1)::bool AS taken;

-- ===== 目录导入（models.dev 镜像 → 本地实体）=====

-- name: ListCatalogItems :many
-- 管理端导入页的搜索：在本地镜像里按名字 / id / canonical / 厂商找。
--
-- 为什么是本地镜像而不是实时查：models.dev 只提供整包 JSON（约 10MB），没有搜索接口。
-- 上游不可用时本站照常工作——这是模型域 §4.6.1 的第一条原则。
--
-- `includeDeprecated=false` 默认过滤掉上游 `status=deprecated` 的条目（实测 904 条）。
-- 这与我们的 `models.status='deprecated'`（合并退役）是两套语义，不可混用：
-- 上游弃用说的是「厂商不再维护」，我们弃用说的是「这条已经并到别人身上」。
SELECT i.id,
       i.provider_id,
       i.provider_name,
       i.external_id,
       i.name,
       i.canonical_id,
       i.vendor_slug,
       i.family,
       i.model_type,
       i.upstream_status,
       i.context_window,
       i.max_output,
       i.cost_in,
       i.cost_out,
       i.release_date,
       i.knowledge_cutoff,
       i.modalities_in,
       i.modalities_out,
       i.description,
       i.fetched_at,
       CASE WHEN EXISTS (
           SELECT 1 FROM model_external_refs r
           WHERE r.source = i.source
             AND r.provider_id = i.provider_id
             AND r.external_id = i.external_id
       ) THEN true ELSE false END AS imported
FROM model_catalog_items i
WHERE (sqlc.narg('q')::text IS NULL
       OR i.name ILIKE '%' || sqlc.narg('q')::text || '%'
       OR i.external_id ILIKE '%' || sqlc.narg('q')::text || '%'
       OR COALESCE(i.canonical_id, '') ILIKE '%' || sqlc.narg('q')::text || '%'
       OR i.provider_name ILIKE '%' || sqlc.narg('q')::text || '%')
  AND (sqlc.narg('include_deprecated')::boolean IS TRUE
       OR COALESCE(i.upstream_status, '') <> 'deprecated')
ORDER BY i.provider_name, i.name, i.id
LIMIT sqlc.arg(row_limit)::int;

-- name: GetCatalogItem :one
-- 按 (source, provider_id, external_id) 取一条镜像行：导入的 preview 与 apply 都以它为准。
--
-- 不按 id 取，是因为 id 是本库的实现细节；上游身份三元组才是跨实例稳定的东西
--（镜像重建、换库之后 id 都会变）。
SELECT id,
       source,
       provider_id,
       provider_name,
       external_id,
       name,
       canonical_id,
       vendor_slug,
       family,
       model_type,
       upstream_status,
       context_window,
       max_output,
       cost_in,
       cost_out,
       release_date,
       knowledge_cutoff,
       open_weights,
       modalities_in,
       modalities_out,
       description
FROM model_catalog_items
WHERE source = sqlc.arg(source)::text
  AND provider_id = sqlc.arg(provider_id)::text
  AND external_id = sqlc.arg(external_id)::text;

-- name: GetModelDiffTarget :one
-- 「补全」模式要先知道这条上游条目认领到了哪个本地模型（没有就是新建）。
--
-- 匹配链路的前两步（模型域 §4.6.4）：先按 refs 精确命中，再按 canonical 命中任意 ref。
-- 后面的别名 / slug / name 匹配交给 ResolveModelRefs——那条 SQL 已经覆盖了三种写法，
-- 这里不重复一套优先级。
SELECT r.model_id
FROM model_external_refs r
WHERE (r.source = sqlc.arg(source)::text
       AND r.provider_id = sqlc.arg(provider_id)::text
       AND r.external_id = sqlc.arg(external_id)::text)
   OR (r.source = sqlc.arg(source)::text
       AND sqlc.narg('canonical_id')::text IS NOT NULL
       AND r.canonical_id = sqlc.narg('canonical_id')::text)
ORDER BY (r.provider_id = sqlc.arg(provider_id)::text) DESC, r.id
LIMIT 1;

-- name: ListModelSpecsForFields :one
-- 补全时要知道「当前值 + 字段来源 + 是否锁定」，才能算出哪些字段可以勾。
SELECT ms.context_window,
       ms.max_output,
       ms.cost_in,
       ms.cost_out,
       ms.release_date,
       ms.knowledge_cutoff,
       ms.open_weights,
       ms.modalities_in,
       ms.modalities_out,
       ms.license
FROM model_specs ms
WHERE ms.model_id = $1;

-- name: ListLockedSpecFields :many
-- 锁定字段与人工来源字段：同步**不许**覆盖它们（模型域 §4.6.1 第 4 条「手工优先」）。
SELECT field, source, locked
FROM model_spec_fields
WHERE model_id = $1;

-- name: UpsertModelSpecs :exec
-- 应用 diff 时写档案。`source` 只表示「主要来源」，逐字段来源在 model_spec_fields。
INSERT INTO model_specs (
    model_id, context_window, max_output, cost_in, cost_out, release_date,
    knowledge_cutoff, open_weights, modalities_in, modalities_out, license,
    source, fetched_at
) VALUES (
    sqlc.arg(model_id), sqlc.narg(context_window), sqlc.narg(max_output),
    sqlc.narg(cost_in), sqlc.narg(cost_out), sqlc.narg(release_date),
    sqlc.narg(knowledge_cutoff), sqlc.narg(open_weights),
    sqlc.arg(modalities_in), sqlc.arg(modalities_out),
    sqlc.narg(license), sqlc.arg(source), now()
)
ON CONFLICT (model_id) DO UPDATE SET
    context_window   = COALESCE(EXCLUDED.context_window, model_specs.context_window),
    max_output       = COALESCE(EXCLUDED.max_output, model_specs.max_output),
    cost_in          = COALESCE(EXCLUDED.cost_in, model_specs.cost_in),
    cost_out         = COALESCE(EXCLUDED.cost_out, model_specs.cost_out),
    release_date     = COALESCE(EXCLUDED.release_date, model_specs.release_date),
    knowledge_cutoff = COALESCE(EXCLUDED.knowledge_cutoff, model_specs.knowledge_cutoff),
    open_weights     = COALESCE(EXCLUDED.open_weights, model_specs.open_weights),
    modalities_in    = CASE WHEN EXCLUDED.modalities_in  <> '' THEN EXCLUDED.modalities_in  ELSE model_specs.modalities_in  END,
    modalities_out   = CASE WHEN EXCLUDED.modalities_out <> '' THEN EXCLUDED.modalities_out ELSE model_specs.modalities_out END,
    license          = COALESCE(EXCLUDED.license, model_specs.license),
    fetched_at       = now();

-- name: UpsertSpecField :exec
-- 逐字段来源与锁定（模型域 §4.6.5）。应用后每个被勾选的字段都留一行。
INSERT INTO model_spec_fields (model_id, field, source, locked)
VALUES (sqlc.arg(model_id), sqlc.arg(field), sqlc.arg(source), false)
ON CONFLICT (model_id, field) DO UPDATE SET
    source = EXCLUDED.source,
    updated_at = now();

-- name: UpsertModelExternalRef :exec
-- 认领上游条目。唯一键 `(source, provider_id, external_id)` 保证一条上游条目只能被
-- 一个本地模型认领——重复认领会造出两个模型指向同一个上游，是模型域 §4.7.4 明令禁止的。
INSERT INTO model_external_refs (model_id, source, provider_id, provider_name, external_id, canonical_id, is_official)
VALUES (sqlc.arg(model_id), sqlc.arg(source), sqlc.arg(provider_id), sqlc.arg(provider_name),
        sqlc.arg(external_id), sqlc.narg(canonical_id), sqlc.arg(is_official))
ON CONFLICT (source, provider_id, external_id) DO UPDATE SET
    model_id = EXCLUDED.model_id,
    provider_name = EXCLUDED.provider_name,
    canonical_id = EXCLUDED.canonical_id,
    is_official = EXCLUDED.is_official;

-- name: UpdateModelFromCatalog :exec
-- 导入新建后把展示名 / 描述 / 厂商补上。
--
-- 只改这三列：`slug` 与 `name` 是公开身份（slug 创建后不可变，name 是展示名），
-- `status` / `resolution` 由「导入即 active」的规则单独设——把它们混进这条 UPDATE
-- 会让「补全档案」顺手改掉状态。
UPDATE models
SET name        = sqlc.arg(name),
    description = sqlc.arg(description),
    vendor      = sqlc.narg(vendor),
    updated_at  = GREATEST(now(), models.created_at)
WHERE id = sqlc.arg(id);

-- name: CreateModelFromCatalog :one
-- 导入新建：落一个 active 的模型（模型域 §4.6.5「新建」模式）。
--
-- `status='active'` 而不是 `candidate`：目录导入是有人对着上游条目点的，不是作者随手填的。
-- 这与作者手填建 `candidate` 是两条路，状态不同正是为了区分「谁给它背过书」。
INSERT INTO models (slug, name, vendor, status, resolution, description)
VALUES (
    sqlc.arg(slug),
    sqlc.arg(name),
    sqlc.narg(vendor),
    'active',
    'exact',
    sqlc.arg(description)
)
RETURNING id;

-- name: GetModelByID :one
-- 导入后回读，把 id 变成契约里的对象。
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
WHERE m.id = $1;
