-- 0040：基准导入的列集与治理登记（#526；列集由 #520 决议）。
--
-- 全部是**纯增量**：新列都可空或带默认，不动既有列，已有行（目前 0 行）不受影响。

-- 1) 陈列档（#520 决议第 1 条）。
--    `table` 才允许把分数写进 `benchmark_scores`：`GET /api/v1/models/{slug}` 会把那些行
--    原样吐成 JSON，而 `cite` / `link` 档的源（如 Artificial Analysis）明确禁止经 API 分发。
ALTER TABLE benchmarks
    ADD COLUMN display_tier text NOT NULL DEFAULT 'table'
        CONSTRAINT ck_benchmarks_display_tier CHECK (display_tier IN ('table', 'cite', 'link'));

-- 2) 署名三列（决议第 2 条）：页面脚注统一读它们，后端不写「来源 == xx」这类判断。
ALTER TABLE benchmarks
    ADD COLUMN license     text NOT NULL DEFAULT '',
    ADD COLUMN license_url text NOT NULL DEFAULT '',
    ADD COLUMN attribution text NOT NULL DEFAULT '';

-- 3) 区间与样本量（决议第 3 条）。全部可空：缺失时页面显示 `—`，**不补零、不推断**。
ALTER TABLE benchmark_scores
    ADD COLUMN sample_size       bigint,
    ADD COLUMN uncertainty_lower double precision,
    ADD COLUMN uncertainty_upper double precision;

-- 上下界写反了是录入事故，不是数据形态；让它进不了库。
ALTER TABLE benchmark_scores
    ADD CONSTRAINT ck_benchmark_scores_uncertainty CHECK (
        uncertainty_lower IS NULL OR uncertainty_upper IS NULL
        OR uncertainty_lower <= uncertainty_upper
    );

-- 4) 权限键 `benchmark.import`（#526）。
--    不复用 `model.import`：那个改的是模型档案，这个改的是**共享的第三方基准目录**，
--    一次导入会影响所有引用该榜的模型页。理由与 0020 同。
INSERT INTO permissions (key, description) VALUES
    ('benchmark.import', '导入第三方基准快照（#526）')
ON CONFLICT (key) DO NOTHING;

INSERT INTO role_permissions (role_key, permission_key) VALUES
    ('admin', 'benchmark.import')
ON CONFLICT DO NOTHING;

-- 5) 审计实体类型 `benchmark`（标准对象 §EN-6 第 16 条）。
--    导入的每一行都写 `create` / `update` 审计，没有实体类型就落不了账。
INSERT INTO audit_entity_types (key, description, table_name, route_template) VALUES
    ('benchmark', '第三方基准', 'benchmarks', '/admin/benchmarks/{id}')
ON CONFLICT (key) DO NOTHING;
