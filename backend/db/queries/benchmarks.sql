-- 第三方基准快照导入（#526）。
--
-- 四张表按 §4.5 的五步 upsert 逐层落地：benchmarks(slug) → versions(benchmark_id, version)
-- → metrics(version_id, key) → scores(metric_id, external_name_key)。
--
-- `(xmax = 0)` 是 PostgreSQL 的既有写法：INSERT 走的分支 xmax 为 0，UPDATE 分支非 0。
-- 用它就能在同一句里区分「新建」与「更新」，dryRun 的条数统计因此不需要第二次查询。

-- name: UpsertBenchmark :one
INSERT INTO benchmarks (slug, name, publisher, homepage_url, category, description,
                         display_tier, license, license_url, attribution)
VALUES (@slug, @name, @publisher, @homepage_url, @category, @description,
        @display_tier, @license, @license_url, @attribution)
ON CONFLICT (slug) DO UPDATE SET
    name         = EXCLUDED.name,
    publisher    = EXCLUDED.publisher,
    homepage_url = EXCLUDED.homepage_url,
    category     = EXCLUDED.category,
    description  = EXCLUDED.description,
    display_tier = EXCLUDED.display_tier,
    license      = EXCLUDED.license,
    license_url  = EXCLUDED.license_url,
    attribution  = EXCLUDED.attribution,
    updated_at   = now()
RETURNING id, (xmax = 0) AS inserted;

-- name: UpsertBenchmarkVersion :one
INSERT INTO benchmark_versions (benchmark_id, version, as_of, source_type, coverage,
                                participant_count, score_min, score_max, source_url, note)
VALUES (@benchmark_id, @version, @as_of, @source_type, @coverage,
        sqlc.narg(participant_count), sqlc.narg(score_min), sqlc.narg(score_max),
        @source_url, @note)
ON CONFLICT (benchmark_id, version) DO UPDATE SET
    as_of             = EXCLUDED.as_of,
    source_type       = EXCLUDED.source_type,
    coverage          = EXCLUDED.coverage,
    participant_count = EXCLUDED.participant_count,
    score_min         = EXCLUDED.score_min,
    score_max         = EXCLUDED.score_max,
    source_url        = EXCLUDED.source_url,
    note              = EXCLUDED.note
RETURNING id, (xmax = 0) AS inserted;

-- name: UpsertBenchmarkMetric :one
INSERT INTO benchmark_metrics (version_id, key, label, metric_kind, unit,
                               scale_min, scale_max, higher_is_better, is_primary, sort, note)
VALUES (@version_id, @key, @label, @metric_kind, @unit,
        sqlc.narg(scale_min), sqlc.narg(scale_max), @higher_is_better, @is_primary, @sort, @note)
ON CONFLICT (version_id, key) DO UPDATE SET
    label            = EXCLUDED.label,
    metric_kind      = EXCLUDED.metric_kind,
    unit             = EXCLUDED.unit,
    scale_min        = EXCLUDED.scale_min,
    scale_max        = EXCLUDED.scale_max,
    higher_is_better = EXCLUDED.higher_is_better,
    is_primary       = EXCLUDED.is_primary,
    sort             = EXCLUDED.sort,
    note             = EXCLUDED.note
RETURNING id, (xmax = 0) AS inserted;

-- name: UpsertBenchmarkScore :one
-- `model_id` 为 NULL 表示这条外部名没解析到任何模型：保留原始分与名次，
-- 只把 `match_status` 标成 unmatched，等别名补上之后重导一次即可。
INSERT INTO benchmark_scores (metric_id, model_id, external_name, external_name_key,
                              match_status, raw_score, rank, ranked, source_url, note,
                              sample_size, uncertainty_lower, uncertainty_upper)
VALUES (@metric_id, sqlc.narg(model_id), @external_name, @external_name_key,
        @match_status, @raw_score, sqlc.narg(rank), @ranked, @source_url, @note,
        sqlc.narg(sample_size), sqlc.narg(uncertainty_lower), sqlc.narg(uncertainty_upper))
ON CONFLICT (metric_id, external_name_key) DO UPDATE SET
    model_id          = EXCLUDED.model_id,
    external_name     = EXCLUDED.external_name,
    match_status     = EXCLUDED.match_status,
    raw_score         = EXCLUDED.raw_score,
    rank              = EXCLUDED.rank,
    ranked            = EXCLUDED.ranked,
    source_url        = EXCLUDED.source_url,
    note              = EXCLUDED.note,
    sample_size       = EXCLUDED.sample_size,
    uncertainty_lower = EXCLUDED.uncertainty_lower,
    uncertainty_upper = EXCLUDED.uncertainty_upper,
    retrieved_at      = now()
RETURNING id, (xmax = 0) AS inserted;

-- name: ResolveModelForBenchmark :one
-- 外部模型名 → 站内模型（票上原文：slug → alias → name，同形态取小的 model_id）。
--
-- 键统一用 `external_name_key`（去空格、小写，见导入侧的 NormalizeNameKey）；别名表里存的
-- 也是规范写法，所以三路比较用同一个键。已合并的档（merged_into_id 非空）与废弃档不参与
-- —— 把分数挂到一个已合并的壳上，模型页会读不到它。
SELECT m.id
FROM models m
WHERE m.status <> 'deprecated'
  AND m.merged_into_id IS NULL
  AND (
      EXISTS (SELECT 1 FROM model_aliases a WHERE a.model_id = m.id AND a.alias = @key::text)
      OR m.slug = @key::text
      OR lower(m.name) = @key::text
  )
ORDER BY
    CASE
        WHEN EXISTS (SELECT 1 FROM model_aliases a2 WHERE a2.model_id = m.id AND a2.alias = @key::text) THEN 0
        WHEN m.slug = @key::text THEN 1
        ELSE 2
    END,
    m.id
LIMIT 1;
-- ---------- 读取：目录与详情（#427）----------

-- name: ListBenchmarks :many
-- 目录一页：只取列表页要的五列，外加「最新版本日期」这个派生值。
--
-- 不复用详情那句 SQL：列表页拿全字段再丢掉，等于每次翻页都读一遍用不上的列。
-- 没有任何版本时 `latest_as_of` 为 NULL —— 那是「刚收录、还没导数据」的正常状态，不是缺数据。
SELECT b.slug, b.name, b.publisher, b.category, b.display_tier,
       (SELECT max(bv.as_of) FROM benchmark_versions bv
         WHERE bv.benchmark_id = b.id AND bv.status = 'active')::date AS latest_as_of
FROM benchmarks b
WHERE b.status = 'active'
ORDER BY b.name, b.id;

-- name: ListBenchmarkOverview :many
-- 模型 × 基准总览（#571）：一行一个模型、一列一张榜，格子是原始分 + 名次。
--
-- 从 `benchmarks` 出发、LEFT JOIN 分数：**指标存在但一条匹配分数都没有时，列本身仍要出现**
-- （列头照常渲染，只是这一列没有格子）。反过来写（从 scores 出发）会把空列整列丢掉，
-- 页面就分不清「这张榜一张分数都没有」和「这张榜没接进来」。
--
-- 口径与契约 `GET /api/v1/benchmarks/overview` 的 description 是同一份，改一处要同步另一处：
-- 榜 `table` 档 + 版本最新非自报 + 指标取主指标 + 分数 matched / ranked / model_id 非空。
SELECT b.slug AS benchmark_slug,
       b.name AS benchmark_name,
       bm.unit,
       bv.version,
       m.slug AS model_slug,
       m.name AS model_name,
       bs.raw_score,
       bs.rank
FROM benchmarks b
JOIN LATERAL (
    SELECT v.id, v.version
    FROM benchmark_versions v
    WHERE v.benchmark_id = b.id
      AND v.status = 'active'
      AND v.source_type <> 'self_reported'
    ORDER BY v.as_of DESC, v.id DESC
    LIMIT 1
) bv ON TRUE
JOIN LATERAL (
    SELECT mt.id, mt.unit
    FROM benchmark_metrics mt
    WHERE mt.version_id = bv.id
      AND mt.is_primary
    LIMIT 1
) bm ON TRUE
LEFT JOIN benchmark_scores bs
       ON bs.metric_id = bm.id
      AND bs.match_status = 'matched'
      AND bs.model_id IS NOT NULL
      AND bs.ranked
LEFT JOIN models m ON m.id = bs.model_id
WHERE b.status = 'active'
  AND b.display_tier = 'table'
ORDER BY b.name, b.id, m.name, m.id;

-- name: GetBenchmarkBySlug :one
SELECT id, slug, name, publisher, homepage_url, category, description,
       display_tier, license, license_url, attribution
FROM benchmarks
WHERE slug = @slug AND status = 'active';

-- name: ListBenchmarkVersions :many
-- 版本历史：全量、按日期倒序。`self_reported` 照常返回，由页面分组（MODEL-020）。
SELECT id, version, as_of, source_type, coverage, participant_count, source_url
FROM benchmark_versions
WHERE benchmark_id = @benchmark_id AND status = 'active'
ORDER BY as_of DESC, id DESC;

-- name: ListBenchmarkMetricsWithScores :many
-- 某一版的指标表。**LEFT JOIN**：指标存在但一条分数都没有时仍要出行（空榜要正常展示）。
--
-- 排序：主榜在前、再按 sort、最后按名次（没有名次的排在后面）。
SELECT bm.key, bm.label, bm.unit, bm.is_primary, bm.higher_is_better,
       bs.external_name, bs.raw_score, bs.rank, bs.sample_size,
       bs.uncertainty_lower, bs.uncertainty_upper, bs.match_status,
       m.slug AS model_slug, m.name AS model_name
FROM benchmark_metrics bm
LEFT JOIN benchmark_scores bs ON bs.metric_id = bm.id
LEFT JOIN models m ON m.id = bs.model_id
WHERE bm.version_id = @version_id
ORDER BY bm.is_primary DESC, bm.sort, bm.key, bs.rank NULLS LAST, bs.raw_score DESC;
