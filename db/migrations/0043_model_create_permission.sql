-- 0041：新增权限 `model.create`（手动新建一个直接可用的模型）。
--
-- 为什么不复用 `model.import`：那个管的是「把上游目录里已有的条目搬进来」——
-- 依据是上游的一条记录，管理员做的是勾选。这个方法没有上游依据：名字是人填的，
-- 点下去就是**本站背书**（直接 active）。两件事的风险面不同：给了导入权不等于
-- 愿意让人凭空往共享目录里造条目。
--
-- 为什么不复用 `entity.edit`：它管「这条内容能不能改」，对象是**作品**；这里改的是
-- 所有版本共享的模型目录。与 0020 的理由一致。
--
-- 与 `proposal.review` 也分开：裁决提案是「判断别人提的对不对」，手动新建是
-- 「自己认定这条存在」。
--
-- 治理域 §4.3 的权限清单同步 24 → 25（#597 加 `model.govern` 时再 → 26）。

INSERT INTO permissions (key, description) VALUES
    ('model.create', '手动新建模型（管理员点下去就是背书，直接 active）')
ON CONFLICT (key) DO NOTHING;

-- 基线里 `SELECT 'admin', key FROM permissions` 只覆盖当时已存在的那些，
-- 后加的权限要显式补绑一次（与 0011 / 0020 同一处置）。
INSERT INTO role_permissions (role_key, permission_key) VALUES
    ('admin', 'model.create')
ON CONFLICT DO NOTHING;
