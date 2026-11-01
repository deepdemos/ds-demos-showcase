-- 70-views.sql（抽取自 docs 的 DDL 块；后续以本文件为准）

-- ===== demo_tasks  （旧版已被下面带 task_overrides 的版本取代，勿复原）=====

CREATE VIEW demo_tasks AS
SELECT d.id AS demo_id, tp.task_id
FROM demos d
JOIN demo_revisions r ON r.id = d.current_revision_id
JOIN task_prompts tp ON tp.prompt_id = r.prompt_id
WHERE r.prompt_id IS NOT NULL
UNION
SELECT demo_id, task_id FROM demo_task_overrides;

-- ===== demo_platforms  (docs/标签域数据结构设计.md:470) =====
-- demo 级视图：保留原表名，读路径不用改（按当前 revision 派生，G18）
CREATE VIEW demo_platforms AS
SELECT d.id AS demo_id, rp.platform_id
  FROM demos d JOIN demo_revision_platforms rp ON rp.revision_id = d.current_revision_id;

-- ===== demo_platform_configs  (docs/标签域数据结构设计.md:542) =====
-- demo 级视图：保留原表名，读路径不用改（按当前 revision 派生）
CREATE VIEW demo_platform_configs AS
SELECT d.id AS demo_id, rc.config_id
  FROM demos d JOIN demo_revision_platform_configs rc ON rc.revision_id = d.current_revision_id;

-- ===== demo_skills  (docs/标签域数据结构设计.md:658) =====
-- demo 级视图：保留原表名，读路径不用改（按当前 revision 派生）
CREATE VIEW demo_skills AS
SELECT d.id AS demo_id, rs.skill_id
  FROM demos d JOIN demo_revision_skills rs ON rs.revision_id = d.current_revision_id;

-- ===== demo_models  (docs/模型域设计.md:138) =====
CREATE VIEW demo_models AS
SELECT d.id AS demo_id, rm.model_id, rm.provider_id, rm.serving_note, rm.source
  FROM demos d
  JOIN demo_revision_models rm ON rm.revision_id = d.current_revision_id;
