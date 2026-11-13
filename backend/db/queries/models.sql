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
-- 本来就是 `model_offerings` 里 `authority = 'models.dev'` 那些行的事实。
-- 多加一列等于给同一件事两个出处。
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
           WHEN EXISTS (SELECT 1 FROM model_offerings r WHERE r.model_id = m.id AND r.authority = 'models.dev') THEN 'models.dev'
           ELSE 'manual'
       END::text AS source,
       m.description,
       m.created_at,
       m.updated_at
FROM models m
LEFT JOIN model_merge_targets mt ON mt.model_id = m.id
LEFT JOIN models t ON t.id = mt.terminal_id AND t.id <> m.id
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
-- 字段面与 ListModels **逐字一致**（前 11 列同名同序；ListModels 的 SELECT 正好 11 列），**外加这一版用的 checkpoint**。
-- checkpoint 挂在「版本 × 模型」这条关系上（`demo_revision_models.checkpoint_id`），
-- 不是模型的属性：同一个模型在两个作品里可以是不同的一版。它照样走 `Model.checkpoint`
-- 这个字段（契约里 Model 就带它，目录那一侧恒为 null）——不另开一个 schema，是因为
-- allOf 摊平后会把全局枚举常量一起改名，生成物的噪声远大于收益。
--
-- 三个 checkpoint 列的 NULL 有意义：`checkpoint_id IS NULL` 就是「这一版没说用的是
-- 哪一版模型」，展示要落到「未固定」，不能默认成最新版。LEFT JOIN 保证没填
-- checkpoint 的声明照样出现（否则填不填会改变「作品用了几行模型」）。
--
-- 排序按名字，让同一个作品两次打开的顺序一致。
SELECT mo.id,
       mo.slug,
       mo.name,
       COALESCE(mo.vendor, '')::text AS vendor,
       mo.status,
       mo.resolution,
       COALESCE(t.slug, '')::text AS merged_into_slug,
       CASE
           WHEN EXISTS (SELECT 1 FROM model_offerings r WHERE r.model_id = mo.id AND r.authority = 'models.dev') THEN 'models.dev'
           ELSE 'manual'
       END::text AS source,
       mo.description,
       mo.created_at,
       mo.updated_at,
       rm.checkpoint_id,
       c.label AS checkpoint_label,
       c.released_on AS checkpoint_released_on
FROM demo_revision_models rm
JOIN models mo ON mo.id = rm.model_id
LEFT JOIN model_merge_targets mt ON mt.model_id = mo.id
LEFT JOIN models t ON t.id = mt.terminal_id AND t.id <> mo.id
LEFT JOIN model_checkpoints c ON c.id = rm.checkpoint_id
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
-- `rank` 决定同名不同形态时谁优先：slug(0) > alias(1) > name(2)。同 rank 再按 `model_id`
-- 取小的（已合并的取归宿 id，见下面）——保证结果稳定，否则同一份输入两次解析
-- 可能命中不同的行，作者会看到模型莫名换掉。
--
-- 别名允许歧义：`model_aliases` 的主键是 `(alias, model_id)`，同一个写法可以同时指向多个
-- 模型，这条规则就是唯一的裁决者，库层不再拦。
--
-- 已合并的模型（`status='deprecated'` 且 `merged_into_id` 非空）沿指针**走到尽头**，返回
-- 最终归宿 id：作者手里可能还留着旧名字，报错「模型不存在」既不准确也没帮助。A→B→C 的
-- 两跳链以前会停在 B（那时只走一跳），B 也被合并掉之后这条写法就等于指向一个「已合并、
-- 归宿也已合并」的中间态，按 id 查得到、按名字解析到 B 的归宿却不是 C。
--
-- 环（A→B→A）用 `depth < 20` 兜住：20 是大类「实体合并链最大深度」的登记值
-- （`docs/运行手册-可变常量.md` 第 10 条），超出的行按深度 20 的那一层当终点，保证任何数据
-- 形状都能返回一行、不会换成一次无限递归。自指（`merged_into_id = id`）在库里已被
-- `ck_models_no_self` 挡住，这里不假设上游一定拦住了。
--
-- 指针悬空时退回被合并的那个 id，而不是解析成「没有这个模型」：`models.merged_into_id`
-- 没有外键（见 `docs/已知松点.md`），归宿被硬删之后指针会留在原处；此时若返回空，
-- 作者手里那个旧名字就会重新变成一个待建的候选模型，等于凭空多出一行。
WITH RECURSIVE input AS (
    SELECT DISTINCT lower(btrim(r))::text AS ref
    FROM unnest(sqlc.arg(refs)::text[]) AS r
    WHERE btrim(r) <> ''
),
matched AS (
    SELECT i.ref,
           m.id AS model_id,
           m.status,
           m.merged_into_id,
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
),
walk AS (
    SELECT ref, model_id, status, merged_into_id, rank, 0 AS depth
    FROM matched
    UNION ALL
    SELECT w.ref, t.id, t.status, t.merged_into_id, w.rank, w.depth + 1
    FROM walk w
    JOIN models t ON t.id = w.merged_into_id
    WHERE w.status = 'deprecated'
      AND w.merged_into_id IS NOT NULL
      AND w.depth < 20
)
SELECT DISTINCT ON (ref) ref::text AS ref, model_id
FROM walk w
WHERE w.status <> 'deprecated'
   OR w.merged_into_id IS NULL
   OR w.depth >= 20
   OR NOT EXISTS (SELECT 1 FROM models t WHERE t.id = w.merged_into_id)
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
-- `(alias, model_id)` 是复合主键：同一个写法**允许**先后指到多个模型（作者手填的名字撞名
-- 是常态，两个模型共用一个写法也该写得进来），主键只保证「同一个写法不会重复挂到同一个模型
-- 上」。有歧义时取哪一条由 `ResolveModelRefs` 裁决（同 rank 取小的 `model_id`），不靠库层把写
-- 拦掉：库层只会把「有歧义」变成「写不进去」。
INSERT INTO model_aliases (alias, model_id)
VALUES (sqlc.arg(alias), sqlc.arg(model_id))
ON CONFLICT (alias, model_id) DO NOTHING;

-- name: ModelSlugTaken :one
-- slug 是公开 URL 的一部分、创建后不可变，生成时先查一遍再落库。
SELECT EXISTS (SELECT 1 FROM models WHERE slug = $1)::bool AS taken;

-- ===== 模型版本（checkpoint）=====

-- name: InsertModelCheckpoint :one
-- 给一个模型登记一版快照（「v4.1-flash 的 1017 那版」）。
--
-- 为什么版本是模型的子表而不是一张独立的模型：版本与模型是同一个东西的两层粒度，
-- 拉平成两个模型会让「合并 / 去重 / 对比」把版本差异当成模型差异（见 0029 迁移头）。
-- `(model_id, label)` 唯一：同一个模型下同名版本只该有一行；不同模型下同名版本互不相干。
--
-- `ON CONFLICT DO NOTHING` 不带条件地让「同一个版本登记两次」变成无害操作：
-- 冲突时不返回行，调用方据此回查（与 `CreateCandidateModel` 同一套路），不把它当失败。
-- `released_on` 走 `sqlc.narg`：上游日期常常只知道月份，空着比编一个准，NULL 就是「日期不明」。
INSERT INTO model_checkpoints (model_id, label, released_on, notes)
VALUES (
    sqlc.arg(model_id),
    sqlc.arg(label),
    sqlc.narg(released_on),
    sqlc.arg(notes)
)
ON CONFLICT (model_id, label) DO NOTHING
RETURNING id;

-- name: FindModelCheckpoint :one
-- 登记撞唯一键后回查：拿到已存在那版的行 id，让调用方不必区分「刚建的」与「早就有的」。
SELECT id
FROM model_checkpoints
WHERE model_id = sqlc.arg(model_id)
  AND label = sqlc.arg(label);

-- name: ListModelCheckpoints :many
-- 列某个模型的全部版本：新的在前，日期不明（NULL）排最后，同一天按 label 定序。
--
-- NULLS LAST 要显式写：Postgres 的 DESC 默认是 NULLS FIRST，不写会让「日期不明」顶到最前面，
-- 给人的印象是「最新的那版不知道什么时候发的」。
SELECT id, model_id, label, released_on, notes, created_at
FROM model_checkpoints
WHERE model_id = sqlc.arg(model_id)
ORDER BY released_on DESC NULLS LAST, label;

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
           SELECT 1 FROM model_offerings r
           WHERE r.authority = i.source
             AND r.provider_id = i.provider_id
             AND r.external_id = i.external_id
       ) THEN true ELSE false END AS imported
FROM model_catalog_items i
WHERE (sqlc.narg('q')::text IS NULL
       OR i.name ILIKE '%' || sqlc.narg('q')::text || '%' ESCAPE '\'
       OR i.external_id ILIKE '%' || sqlc.narg('q')::text || '%' ESCAPE '\'
       OR COALESCE(i.canonical_id, '') ILIKE '%' || sqlc.narg('q')::text || '%' ESCAPE '\'
       OR i.provider_name ILIKE '%' || sqlc.narg('q')::text || '%' ESCAPE '\')
  AND (sqlc.narg('include_deprecated')::boolean IS TRUE
       OR COALESCE(i.upstream_status, '') <> 'deprecated')
ORDER BY i.provider_name, i.name, i.id
LIMIT sqlc.arg(row_limit)::int
OFFSET sqlc.arg(row_offset)::int;

-- name: CountCatalogItems :one
-- 与 `ListCatalogItems` 同一组筛选条件下的总数（分页要用）。
--
-- 两条 SQL 的 WHERE 必须逐字一致，否则「共 N 条」与翻页到底的条数对不上——
-- 那种偏差在界面上表现为「最后一页少几条」，很难归因。
--
-- `%` / `_` / `\` 由调用方先转义（与作品搜索同一口径），这里只负责 `ESCAPE '\'`。
SELECT count(*)::bigint
FROM model_catalog_items i
WHERE (sqlc.narg('q')::text IS NULL
       OR i.name ILIKE '%' || sqlc.narg('q')::text || '%' ESCAPE '\'
       OR i.external_id ILIKE '%' || sqlc.narg('q')::text || '%' ESCAPE '\'
       OR COALESCE(i.canonical_id, '') ILIKE '%' || sqlc.narg('q')::text || '%' ESCAPE '\'
       OR i.provider_name ILIKE '%' || sqlc.narg('q')::text || '%' ESCAPE '\')
  AND (sqlc.narg('include_deprecated')::boolean IS TRUE
       OR COALESCE(i.upstream_status, '') <> 'deprecated');

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
FROM model_offerings r
WHERE (r.authority = sqlc.arg(authority)::text
       AND r.provider_id = sqlc.arg(provider_id)::text
       AND r.external_id = sqlc.arg(external_id)::text)
   OR (r.authority = sqlc.arg(authority)::text
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

-- name: UpsertModelOffering :exec
-- 登记一条「调用」：某个 authority（现在只有 models.dev，将来还有端点上报）下的
-- 某个 external_id 指的是这个模型。唯一键 `(authority, provider_id, external_id)`
-- 保证同一条上游条目只能被一个本地模型认领——重复认领会造出两个模型指向同一个上游，
-- 是模型域 §4.7.4 明令禁止的。
--
-- 键里为什么要带 provider_id：目录实测 8988 条里只有 4131 个 distinct external_id，
-- 1182 个 external_id 被多个 provider 复用（同一个名字挂在不同 provider 名下）。
-- 去掉 provider 维度会在迁移那一步直接撞唯一约束。
INSERT INTO model_offerings (model_id, authority, provider_id, provider_name, external_id, canonical_id, is_official)
VALUES (sqlc.arg(model_id), sqlc.arg(authority), sqlc.arg(provider_id), sqlc.arg(provider_name),
        sqlc.arg(external_id), sqlc.narg(canonical_id), sqlc.arg(is_official))
ON CONFLICT (authority, provider_id, external_id) DO UPDATE SET
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
           WHEN EXISTS (SELECT 1 FROM model_offerings r WHERE r.model_id = m.id AND r.authority = 'models.dev') THEN 'models.dev'
           ELSE 'manual'
       END::text AS source,
       m.description,
       m.created_at,
       m.updated_at
FROM models m
LEFT JOIN model_merge_targets mt ON mt.model_id = m.id
LEFT JOIN models t ON t.id = mt.terminal_id AND t.id <> m.id
WHERE m.id = $1;

-- ---------------------------------------------------------------------------
-- 目录同步（镜像表 model_catalog_items + 刷新状态 model_source_sync）
--
-- 这几个查询与上面的模型目录读写同属一个域，放在一起是为了让「模型域有哪些 SQL」
-- 只有一个文件要看；同步具体怎么算（diff、快照、定时）在
-- internal/domain/model/sync.go 里。
-- ---------------------------------------------------------------------------

-- ---------------------------------------------------------------------------
-- 模型页读路径（模型域 §5.1）
--
-- 三块独立取数：档案（model_specs）、站内表现（demo_models + demo_stats）、
-- 第三方基准（benchmark_*）。不合并成一个「总分」——三个口径的置信度差得远。
--
-- 站内表现一律**按当前 revision**（G18）：`demo_models` 视图就是
-- `demos ⋈ demo_revision_models(current_revision_id)` 的投影。作者改稿换模型，
-- 归属立刻跟着变，即使线上展示的还是上一版。
-- ---------------------------------------------------------------------------

-- name: GetModelBySlug :one
-- 模型页的主对象。字段面与 `ListModels` 逐字一致，只是按 slug 取。
SELECT m.id,
       m.slug,
       m.name,
       COALESCE(m.vendor, '')::text AS vendor,
       m.status,
       m.resolution,
       COALESCE(t.slug, '')::text AS merged_into_slug,
       CASE
           WHEN EXISTS (SELECT 1 FROM model_offerings r WHERE r.model_id = m.id AND r.authority = 'models.dev') THEN 'models.dev'
           ELSE 'manual'
       END::text AS source,
       m.description,
       m.created_at,
       m.updated_at
FROM models m
LEFT JOIN model_merge_targets mt ON mt.model_id = m.id
LEFT JOIN models t ON t.id = mt.terminal_id AND t.id <> m.id
WHERE m.slug = $1;

-- name: GetModelSpecs :one
-- 档案区。没有行时返回 ErrNoRows——「没有档案」与「档案全是空」是两件事，
-- 前者界面该说「还没有档案」，后者该显示一排「—」。
SELECT model_id,
       context_window,
       max_output,
       param_total_b,
       param_active_b,
       param_kind,
       param_note,
       release_date,
       knowledge_cutoff,
       open_weights,
       cost_in,
       cost_out,
       license,
       modalities_in,
       modalities_out,
       source,
       fetched_at,
       note
FROM model_specs
WHERE model_id = $1;

-- name: ModelSiteStats :one
-- 站内表现：作品数、票数、以及社区分。
--
-- 社区分口径（模型域 §5.2）：
--   score = (wsum + m*C) / (votes + m)      # 贝叶斯收缩
--   C     = 全站票加权均分
--   m     = 各模型票数的中位数（≥1）
--
-- `m` 取中位数而不是固定常数：票数分布长尾时，固定常数会让「只有 1 票」的模型
-- 分数几乎等于它那一票。
--
-- `m` 的下限是 1：所有模型都没票时，`m=0` 会让分母变成 0。
-- 票数为 0 时返回 score = NULL 而不是 0——「没人投过票」与「平均 0 分」在界面上
-- 是两种完全不同的意思，不能靠前端猜。
WITH site AS (
    SELECT COALESCE(SUM(s.rating_avg * s.rating_count), 0)::double precision AS wsum,
           COALESCE(SUM(s.rating_count), 0)::bigint AS votes,
           COUNT(DISTINCT dm.demo_id)::bigint AS demo_count
      FROM demo_models dm
      JOIN demo_public_demos pd ON pd.demo_id = dm.demo_id
      LEFT JOIN demo_stats s ON s.demo_id = dm.demo_id
     WHERE dm.model_id = sqlc.arg(model_id)::bigint
),
global_avg AS (
    SELECT CASE
               WHEN COALESCE(SUM(rating_count), 0) > 0
                   THEN SUM(rating_avg * rating_count) / SUM(rating_count)
               ELSE 0
           END::double precision AS c
      FROM demo_stats
),
per_model AS (
    SELECT dm.model_id, COALESCE(SUM(s.rating_count), 0)::bigint AS votes
      FROM demo_models dm
      JOIN demo_public_demos pd ON pd.demo_id = dm.demo_id
      LEFT JOIN demo_stats s ON s.demo_id = dm.demo_id
     GROUP BY dm.model_id
),
m_prior AS (
    SELECT GREATEST(
               COALESCE(percentile_cont(0.5) WITHIN GROUP (ORDER BY votes), 1),
               1
           )::double precision AS m
      FROM per_model
)
SELECT site.demo_count,
       site.votes,
       global_avg.c AS global_avg,
       m_prior.m AS prior_weight,
       -- 票数为 0 时 score 是「没有意义」而不是 0 分。契约里用 `scored` 布尔表达这件事：
       -- 单靠 score = 0 分不清「没人投过」与「平均分为 0」。
       (site.votes > 0) AS scored,
       COALESCE(
           CASE
               WHEN site.votes = 0 THEN NULL
               ELSE (site.wsum + m_prior.m * global_avg.c) / (site.votes + m_prior.m)
           END,
           0
       )::double precision AS score
  FROM site, global_avg, m_prior;

-- name: ListModelDemos :many
-- 用了这个模型的公开作品。判据与作品库同一份（demo_public_demos），保证
-- 「模型页显示 6 件」与「作品库筛选该模型 6 条」永远一致。
SELECT d.slug,
       d.title,
       d.created_at,
       COALESCE(s.rating_avg, 0)::double precision AS rating_avg,
       COALESCE(s.rating_count, 0)::bigint AS rating_count
  FROM demo_models dm
  JOIN demo_public_demos pd ON pd.demo_id = dm.demo_id
  JOIN demos d ON d.id = dm.demo_id
  LEFT JOIN demo_stats s ON s.demo_id = dm.demo_id
 WHERE dm.model_id = sqlc.arg(model_id)::bigint
 ORDER BY d.created_at DESC, d.id DESC;

-- name: ListModelProviders :many
-- provider 分布（模型域 §5.2.1）。只读展示，**不改变**上面的社区分。
--
-- `provider_id` 为空时归成一行「未标注」，界面上永远排最后：它不是「某个 provider」，
-- 而是「我们不知道是谁」。混在中间会让人以为它是一个服务方。
SELECT dm.provider_id,
       COALESCE(p.slug, '')::text AS provider_slug,
       COALESCE(p.name, '')::text AS provider_name,
       COUNT(DISTINCT dm.demo_id)::bigint AS demo_count,
       COALESCE(SUM(s.rating_count), 0)::bigint AS votes,
       -- NULL 会被 pgx 直接扫描失败（`cannot scan NULL into *float64`），
       -- 所以这里给 0，并在 Go 里按 votes > 0 决定要不要展示。
       COALESCE(
           CASE
               WHEN SUM(s.rating_count) > 0
                   THEN SUM(s.rating_avg * s.rating_count) / SUM(s.rating_count)
               ELSE NULL
           END,
           0
       )::double precision AS avg_rating
  FROM demo_models dm
  JOIN demo_public_demos pd ON pd.demo_id = dm.demo_id
  LEFT JOIN demo_stats s ON s.demo_id = dm.demo_id
  LEFT JOIN providers p ON p.id = dm.provider_id
 WHERE dm.model_id = sqlc.arg(model_id)::bigint
 GROUP BY dm.provider_id, p.slug, p.name
 ORDER BY (dm.provider_id IS NULL), COUNT(DISTINCT dm.demo_id) DESC, provider_name;

-- name: ListModelBenchmarks :many
-- 第三方基准（模型域 §5.3/§5.4 的读取侧）。**只列参与综合分资格的条目**：
-- 榜单与版本都 active、`coverage='full'`、非自报、有 primary 指标、`ranked` 且有名次、
-- 参赛数 ≥ 3。不满足的留在库里但不进页面——把「不参与」的东西摆出来，
-- 读者会以为它参与了。
--
-- 综合分与强弱标记本期不算：它要等归一化方案拍板（模型域 §5.3 明写待定）。
-- 这一层先把「每张榜给了什么」如实摆出来。
SELECT b.slug AS benchmark_slug,
       b.name AS benchmark_name,
       bv.version,
       bv.as_of,
       bv.participant_count,
       bv.source_url,
       bm.key AS metric_key,
       bm.label AS metric_label,
       bm.unit,
       bs.raw_score,
       bs.rank
  FROM benchmark_scores bs
  JOIN benchmark_metrics bm ON bm.id = bs.metric_id
  JOIN benchmark_versions bv ON bv.id = bm.version_id
  JOIN benchmarks b ON b.id = bv.benchmark_id
 WHERE bs.model_id = sqlc.arg(model_id)::bigint
   AND bs.ranked
   AND bs.rank IS NOT NULL
   AND bs.match_status = 'matched'
   AND b.status = 'active'
   AND bv.status = 'active'
   AND bv.coverage = 'full'
   AND bv.source_type <> 'self_reported'
   AND bv.participant_count >= 3
   AND bm.is_primary
 ORDER BY b.name, bv.as_of DESC;

-- name: UpsertCatalogItems :execrows
-- 按条更新镜像：同一 (source, provider_id, external_id) 的行 id 不变，只把内容改成上游这次的样子。
--
-- 为什么不再「先清空再灌」：整表重写让每一行每天都换一个 id，翻页、人工记下的 id、
-- 以及将来任何想引用 catalog 行的东西都会在第二天失效；9000 行也全是死元组。
-- 按条更新把这件事变成「只有真的变了才写」。
--
-- 闸门（结尾那句 WHERE）不是优化，是语义：`fetched_at` 记的是**这一行内容最后一次变化**
-- 的时间，不是「这次同步跑过」的时间。没有闸门，每天都会把 9000 行的 fetched_at
-- 原地刷新一遍，「上游改了」与「我们同步过」就又混成一个时间点了。
--
-- 闸门写成「整行减去 id / source / fetched_at」再比，而不是逐列写字面量：
-- 加列时这里不必再改一次，也就不会出现「SET 里加了、闸门里漏了」这种
-- 「每轮都更新全部行」的静默退化。行比较用 IS DISTINCT FROM 而不是 <>，
-- 所以 NULL → 值、值 → NULL 都算变化（上游把 canonical_id 补上或抹掉是常见的）。
--
-- payload 是一个 JSON 数组，每个元素的键就是下面这些列名（`jsonb_populate_recordset`
-- 按列名取值，多余的键忽略，缺的键给 NULL）。空串不会被当成 NULL，所以可空文本列
-- 在 Go 侧必须序列化成 null 而不是 ""——否则「上游没给」会变成「上游给了一个空串」。
INSERT INTO model_catalog_items (
    source, provider_id, provider_name, external_id, name, canonical_id, vendor_slug,
    family, description, modalities_in, modalities_out, context_window, max_output,
    cost_in, cost_out, release_date, knowledge_cutoff, open_weights, model_type,
    upstream_status, supports_tool_call, supports_reasoning, supports_structured_output,
    supports_attachment, raw_json, fetched_at
)
SELECT @source::text,
       v.provider_id, v.provider_name, v.external_id, v.name, v.canonical_id, v.vendor_slug,
       v.family, v.description, v.modalities_in, v.modalities_out, v.context_window, v.max_output,
       v.cost_in, v.cost_out, v.release_date, v.knowledge_cutoff, v.open_weights, v.model_type,
       v.upstream_status, v.supports_tool_call, v.supports_reasoning, v.supports_structured_output,
       v.supports_attachment, v.raw_json, @fetched_at::timestamptz
FROM jsonb_populate_recordset(NULL::model_catalog_items, @payload::jsonb) AS v
ON CONFLICT (source, provider_id, external_id) DO UPDATE SET
    provider_name              = EXCLUDED.provider_name,
    name                       = EXCLUDED.name,
    canonical_id               = EXCLUDED.canonical_id,
    vendor_slug                = EXCLUDED.vendor_slug,
    family                     = EXCLUDED.family,
    description                = EXCLUDED.description,
    modalities_in              = EXCLUDED.modalities_in,
    modalities_out             = EXCLUDED.modalities_out,
    context_window             = EXCLUDED.context_window,
    max_output                 = EXCLUDED.max_output,
    cost_in                    = EXCLUDED.cost_in,
    cost_out                   = EXCLUDED.cost_out,
    release_date               = EXCLUDED.release_date,
    knowledge_cutoff           = EXCLUDED.knowledge_cutoff,
    open_weights               = EXCLUDED.open_weights,
    model_type                 = EXCLUDED.model_type,
    upstream_status            = EXCLUDED.upstream_status,
    supports_tool_call         = EXCLUDED.supports_tool_call,
    supports_reasoning         = EXCLUDED.supports_reasoning,
    supports_structured_output = EXCLUDED.supports_structured_output,
    supports_attachment        = EXCLUDED.supports_attachment,
    raw_json                   = EXCLUDED.raw_json,
    fetched_at                 = EXCLUDED.fetched_at
WHERE (to_jsonb(model_catalog_items) - 'id' - 'source' - 'fetched_at')
  IS DISTINCT FROM (to_jsonb(EXCLUDED) - 'id' - 'source' - 'fetched_at');

-- name: DeleteCatalogItemsNotIn :exec
-- 上游这次没有的条目从镜像里删掉。
--
-- 只删「本次 payload 里没有的」，不整表删：镜像要跟上游一致，但一致的办法是
-- 「补 + 删」，不是「推倒重来」。调用方保证 keys 非空——空目录在解析那一步就被拒了
-- （`TestIntegrationSyncRejectsEmptyCatalog`），否则这条会删光整张镜像。
--
-- keys 只带 (provider_id, external_id) 两个键：整包 6 MB，为了删几行再传一遍完整
-- payload 不值。实测 9000 行时：旧写法 DELETE ... WHERE source 只要 3.7 ms，但它之后还得
-- 灌 9000 行（199 ms）；这条只带两个键的反连接是 25 ms。
DELETE FROM model_catalog_items c
WHERE c.source = @source::text
  AND NOT EXISTS (
      SELECT 1 FROM jsonb_to_recordset(@keys::jsonb) AS k(provider_id text, external_id text)
      WHERE k.provider_id = c.provider_id
        AND k.external_id = c.external_id
  );

-- name: UpsertModelSourceSync :exec
-- 记账一行：这次同步什么时候试的、成没成、抓到了多少条、payload 指纹是什么。
--
-- 失败时**不更新** `last_success_at` 与 `item_count`：镜像还是上一次成功那份，
-- 这两个值就该停在上一次。否则运维看到「成功时间 = 刚才」，会以为镜像已经更新。
INSERT INTO model_source_sync (source, last_attempt_at, last_success_at, payload_hash, item_count, status, error)
VALUES (
    @source::text,
    now(),
    @last_success_at,
    @payload_hash,
    @item_count,
    @status::text,
    @error::text
)
ON CONFLICT (source) DO UPDATE SET
    last_attempt_at = EXCLUDED.last_attempt_at,
    -- 成功才推 last_success_at；失败保留旧值。
    last_success_at = COALESCE(EXCLUDED.last_success_at, model_source_sync.last_success_at),
    payload_hash    = COALESCE(EXCLUDED.payload_hash, model_source_sync.payload_hash),
    item_count      = COALESCE(EXCLUDED.item_count, model_source_sync.item_count),
    status          = EXCLUDED.status,
    error           = EXCLUDED.error;

-- name: GetModelSourceSync :one
-- 读一个来源的同步状态。从没同步过时返回 ErrNoRows（不返回零值行）：
-- 「有一行但全是零」与「一行都没有」对调用方是两件事，前者会让「上次成功于 0001 年」。
SELECT source, last_attempt_at, last_success_at, payload_hash, item_count, status, error
FROM model_source_sync
WHERE source = @source::text;

-- name: TryModelSyncLock :one
-- 抢一把进程间的锁。
--
-- 定时同步在多实例部署下会同时到点。没有这把锁，两个实例会各拉一次 5.96 MB、
-- 各清一次表——后完成的那次覆盖前一次的记账，而且中间有一段时间表是空的。
--
-- 用 `pg_try_advisory_lock` 而不是 `pg_advisory_lock`：抢不到应该直接跳过这一轮，
-- 而不是排队等——等到的下一轮本来就已经过时了。
SELECT pg_try_advisory_lock(@key::bigint) AS acquired;

-- name: ReleaseModelSyncLock :exec
SELECT pg_advisory_unlock(@key::bigint);

-- name: GetSiteSettingBoolean :one
-- 读一个 boolean 类型的站点参数：有覆盖读覆盖，没有读注册表里的默认值。
--
-- 与 GetSiteSettingNumber 同一形状（缺键直接 ErrNoRows，不返回 false）：
-- 键名拼错时「定时同步永远关着」比「启动就炸」更难发现。
SELECT (COALESCE(s.value, k.default_value))::text::boolean AS value
FROM setting_keys k
LEFT JOIN site_settings s ON s.key = k.key
WHERE k.key = @key::text
  AND k.scope = 'site'
  AND k.value_type = 'boolean'
  AND k.is_active;