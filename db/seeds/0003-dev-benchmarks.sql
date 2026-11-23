-- 开发用第三方基准（#571：模型 × 基准总览）。
--
-- 为什么放种子不放迁移：`benchmarks` 是一张**外部目录**表 —— 正式数据本该由
-- `POST /api/v1/admin/benchmarks/import` 从上游快照导入（模型域 §4.5），不是我们自己的
-- 受控词表。把几张榜塞进迁移，等于让生产库凭空长出一批「我们评测过 HumanEval」的断言。
-- 这里只是让开发库与 CI 有列可点、有格子可看。
--
-- 刻意留了两张**不该进矩阵**的榜（`cite` 档、没有主指标），总览的口径靠它们才验得出来：
-- 只有漂亮数据时，「档位过滤」和「主指标过滤」写错了页面看起来一样正常。
--
-- 全部幂等（ON CONFLICT），可以反复跑。

-- 1) 两张 table 档榜：总览矩阵的列。
INSERT INTO benchmarks (slug, name, publisher, homepage_url, category, description,
                         display_tier, license, attribution)
VALUES
    ('dev-bench-humaneval', 'HumanEval', 'OpenAI',
     'https://github.com/openai/human-eval', 'coding',
     '开发种子：164 道函数级代码生成题，指标 pass@1。',
     'table', 'MIT', 'HumanEval (OpenAI)'),
    ('dev-bench-mmlu', 'MMLU', 'UC Berkeley',
     'https://github.com/hendrycks/test', 'general',
     '开发种子：57 个学科的四选一知识题，指标 accuracy。',
     'table', 'MIT', 'MMLU (Hendrycks et al.)')
ON CONFLICT (slug) DO UPDATE
SET name        = EXCLUDED.name,
    publisher   = EXCLUDED.publisher,
    homepage_url= EXCLUDED.homepage_url,
    category    = EXCLUDED.category,
    description = EXCLUDED.description,
    display_tier= EXCLUDED.display_tier,
    license     = EXCLUDED.license,
    attribution = EXCLUDED.attribution,
    updated_at  = now();

-- 2) 两张反例榜：`cite` 档没有分数可展示；「有指标但没有主指标」也不该占一列。
INSERT INTO benchmarks (slug, name, publisher, category, description, display_tier)
VALUES
    ('dev-bench-cite-sample', '引用档样例榜', '样例发布方', 'general',
     '开发种子：cite 档 —— 只引用、不收录分数，因此不进总览矩阵。', 'cite'),
    ('dev-bench-no-primary', '无主指标样例榜', '样例发布方', 'math',
     '开发种子：有指标但没有标 is_primary 的那一条，因此不进总览矩阵。', 'table')
ON CONFLICT (slug) DO UPDATE
SET name         = EXCLUDED.name,
    publisher    = EXCLUDED.publisher,
    category     = EXCLUDED.category,
    description  = EXCLUDED.description,
    display_tier = EXCLUDED.display_tier,
    updated_at   = now();

-- 3) 每张榜一个版本。
INSERT INTO benchmark_versions (benchmark_id, version, as_of, source_type, coverage,
                                 participant_count, source_url)
SELECT b.id, v.version, v.as_of::date, 'leaderboard', 'full', v.participants, v.url
FROM (VALUES
        ('dev-bench-humaneval',    'v1', '2026-11-10', 4, 'https://github.com/openai/human-eval'),
        ('dev-bench-mmlu',         'v1', '2026-11-05', 3, 'https://github.com/hendrycks/test'),
        ('dev-bench-cite-sample',  'v1', '2026-10-01', 1, ''),
        ('dev-bench-no-primary',   'v1', '2026-10-01', 1, '')
     ) AS v(slug, version, as_of, participants, url)
JOIN benchmarks b ON b.slug = v.slug
ON CONFLICT (benchmark_id, version) DO UPDATE
SET as_of             = EXCLUDED.as_of,
    source_type       = EXCLUDED.source_type,
    coverage          = EXCLUDED.coverage,
    participant_count = EXCLUDED.participant_count,
    source_url        = EXCLUDED.source_url;

-- 4) 指标：两张 table 榜各一个主指标；反例榜分别是「主指标」与「非主指标」。
INSERT INTO benchmark_metrics (version_id, key, label, unit, is_primary, sort)
SELECT bv.id, m.k, m.lb, m.un, m.prim, m.ord
FROM (VALUES
        ('dev-bench-humaneval',   'pass@1',   'pass@1',   '%', true,  0),
        ('dev-bench-mmlu',        'accuracy', 'accuracy', '%', true,  0),
        ('dev-bench-cite-sample', 'score',    '分数',      '',  true,  0),
        ('dev-bench-no-primary',  'elapsed',  '耗时',      's', false, 0)
     ) AS m(slug, k, lb, un, prim, ord)
JOIN benchmarks b ON b.slug = m.slug
JOIN benchmark_versions bv ON bv.benchmark_id = b.id AND bv.version = 'v1'
ON CONFLICT (version_id, key) DO UPDATE
SET label      = EXCLUDED.label,
    unit       = EXCLUDED.unit,
    is_primary = EXCLUDED.is_primary,
    sort       = EXCLUDED.sort;

-- 5) 分数：模型名命中 0002-dev-models.sql 里的开发模型。
--    `deepseek-reasoner` 那一条**不给名次**（rank NULL）—— 源榜没给名次是正常形态，
--    页面要显示分数而不是「—」。
INSERT INTO benchmark_scores (metric_id, model_id, external_name, external_name_key,
                               match_status, raw_score, rank, ranked)
SELECT bm.id, md.id, md.name, lower(md.name), 'matched', s.raw, s.rank, true
FROM (VALUES
        ('dev-bench-humaneval',   'pass@1',   'deepseek-coder',    86.4, 1),
        ('dev-bench-humaneval',   'pass@1',   'deepseek-reasoner', 84.1, NULL),
        ('dev-bench-humaneval',   'pass@1',   'deepseek-chat',     82.9, 2),
        ('dev-bench-humaneval',   'pass@1',   'deepseek-v4-flash', 78.0, 3),
        ('dev-bench-mmlu',        'accuracy', 'deepseek-reasoner', 81.2, 1),
        ('dev-bench-mmlu',        'accuracy', 'deepseek-chat',     78.5, 2),
        ('dev-bench-mmlu',        'accuracy', 'deepseek-v4-flash', 75.0, 3),
        ('dev-bench-cite-sample', 'score',    'deepseek-chat',     91.0, 1),
        ('dev-bench-no-primary',  'elapsed',  'deepseek-chat',     12.5, 1)
     ) AS s(slug, metric_key, model_slug, raw, rank)
JOIN benchmarks b ON b.slug = s.slug
JOIN benchmark_versions bv ON bv.benchmark_id = b.id AND bv.version = 'v1'
JOIN benchmark_metrics bm ON bm.version_id = bv.id AND bm.key = s.metric_key
JOIN models md ON md.slug = s.model_slug
ON CONFLICT (metric_id, external_name_key) DO UPDATE
SET model_id     = EXCLUDED.model_id,
    raw_score    = EXCLUDED.raw_score,
    rank         = EXCLUDED.rank,
    ranked       = EXCLUDED.ranked,
    match_status = EXCLUDED.match_status,
    retrieved_at = now();
