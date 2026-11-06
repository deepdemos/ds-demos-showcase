-- 0020：新增权限 `model.import`（从上游目录（models.dev）预览 diff 并落库）。
--
-- 为什么不复用 `entity.edit`：`entity.edit` 管的是「这条内容能不能改」，对象是**作品**；
-- 导入改的是**共享的模型目录**——一次导入会影响所有引用该模型的版本，写错了不是
-- 一个人的问题。权限键分开，才能在不给编辑权的前提下把导入交给别人。
--
-- 为什么不复用 `demo.curate`：curate 管的是「上架后的可见面」，与模型档案无关。
--
-- 治理域 §4.3 的权限清单同步 22 → 23。

INSERT INTO permissions (key, description) VALUES
    ('model.import', '导入 / 补全模型档案（models.dev）')
ON CONFLICT (key) DO NOTHING;

-- 基线里 `SELECT 'admin', key FROM permissions` 只覆盖当时已存在的那些，
-- 后加的权限要显式补绑一次（与 0011 同一处置）。
INSERT INTO role_permissions (role_key, permission_key) VALUES
    ('admin', 'model.import')
ON CONFLICT DO NOTHING;
