-- 开发用模型主数据。
--
-- 为什么放在种子而不是迁移：`models` 是一张**外部目录**表（正式数据本该从
-- models.dev 那类来源导入，见 `docs/模型域设计.md` §3.4），不是我们自己的受控词表。
-- `demo_types` / `channels` 那种「加一个值 = 加一行」的主数据才进 schema / 迁移。
-- 把外部目录塞进迁移，等于让生产库凭空长出一批「DeepSeek 官方模型」的断言——
-- 而生产该做的是真导入，这里只是让开发库与 CI 有东西可点、可写、可展示。
--
-- 全部按 slug 幂等（ON CONFLICT DO UPDATE），可以反复跑。
-- 生产导入另开一轮：见 `docs/已知松点.md` 第 44 条。

INSERT INTO models (slug, name, vendor, status, resolution, description)
VALUES
    ('deepseek-chat',      'DeepSeek Chat',      'DeepSeek', 'active', 'exact', '通用对话模型，长上下文。'),
    ('deepseek-reasoner',  'DeepSeek Reasoner',  'DeepSeek', 'active', 'exact', '带思维链的推理模型。'),
    ('deepseek-v4-flash',  'DeepSeek V4 Flash',  'DeepSeek', 'active', 'exact', '低延迟的小体量模型，适合批量生成的展示件。'),
    ('deepseek-coder',     'DeepSeek Coder',     'DeepSeek', 'active', 'exact', '代码生成与补全。'),
    ('deepseek-vl2',       'DeepSeek VL2',       'DeepSeek', 'active', 'exact', '视觉语言模型，能读图。'),
    ('deepseek-prover-v2', 'DeepSeek Prover V2', 'DeepSeek', 'active', 'exact', '定理证明，产出可检验的形式化证明。')
ON CONFLICT (slug) DO UPDATE
SET name        = EXCLUDED.name,
    vendor      = EXCLUDED.vendor,
    status      = EXCLUDED.status,
    resolution  = EXCLUDED.resolution,
    description = EXCLUDED.description,
    updated_at  = GREATEST(now(), models.created_at);
