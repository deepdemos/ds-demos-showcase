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
