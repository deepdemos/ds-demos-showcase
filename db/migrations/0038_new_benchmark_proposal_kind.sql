-- 0038：提案 kind 增加 `new_benchmark`（#524 提交入口 / #525 条目提案）。
--
-- 为什么是一条迁移而不是改基线：`db/schema/*.sql` 是冻结基线（等价 0000 号迁移），
-- 基线之后的一切变化**只增不改**（db/migrations/README.md）。
--
-- 两处 CHECK 都要扩，少一处就会在写入那一刻被拒：
--   1. `kind` 列的取值白名单（基线里是匿名列 CHECK，PostgreSQL 自动命名为 proposals_kind_check）；
--   2. `ck_proposals_payload` 的「按 kind 的必填子集」—— `new_benchmark` 只要求 `name`，
--      其余字段（homepage_url / publisher / description / category）都可空，由应用层再收窄。
--
-- 为什么不分开两条迁移：两处 CHECK 同属「这个 kind 存不存在」，拆开只会在中间态里
-- 出现「kind 认了但 payload 不认」的半截状态。

ALTER TABLE proposals DROP CONSTRAINT IF EXISTS proposals_kind_check;

ALTER TABLE proposals ADD CONSTRAINT proposals_kind_check CHECK (kind IN (
    'new_task', 'new_prompt', 'new_model',
    'new_classification', 'new_platform', 'new_platform_config', 'new_skill',
    'new_tag_value',
    'task_match', 'model_alias', 'merge_task', 'merge_model',
    'retag_demo', 'report',
    'new_benchmark'
));

ALTER TABLE proposals DROP CONSTRAINT IF EXISTS ck_proposals_payload;

ALTER TABLE proposals ADD CONSTRAINT ck_proposals_payload CHECK (
    payload IS NULL
 OR (kind = 'new_task'      AND payload ? 'title')
 OR (kind = 'new_prompt'    AND payload ? 'title' AND payload ? 'content')
 OR (kind = 'new_model'     AND payload ? 'name')
 OR (kind = 'new_classification'   AND payload ? 'dimension' AND payload ? 'slug')
 OR (kind = 'new_platform'         AND payload ? 'slug' AND payload ? 'name'
                                  AND jsonb_typeof(payload -> 'is_harness') = 'boolean'
                                  AND jsonb_typeof(payload -> 'forms') = 'array'
                                  AND jsonb_array_length(payload -> 'forms') > 0)
 OR (kind = 'new_platform_config'  AND payload ? 'kind' AND payload ? 'slug'
                                  AND payload ? 'name')
 OR (kind = 'new_skill'            AND payload ? 'slug' AND payload ? 'name')
 OR (kind = 'new_tag_value' AND payload ? 'key' AND payload ? 'value')
 OR (kind = 'task_match'    AND payload ? 'task_id' AND payload ? 'demo_id')
 OR (kind = 'model_alias'   AND payload ? 'model_id' AND payload ? 'alias')
 OR (kind IN ('merge_task', 'merge_model')
                            AND payload ? 'source_id' AND payload ? 'target_id')
 OR (kind = 'retag_demo'    AND payload ? 'demo_id' AND payload ? 'add')
 OR (kind = 'report'        AND payload ? 'target_type' AND payload ? 'target_id')
 OR (kind = 'new_benchmark' AND payload ? 'name')
);
