-- 时间线补 publish 事件。
--
-- 为什么必须加而不是复用已有的 kind：
--   `create` / `approve` / `reject` / `archive` / `restore` / `slug_change` / `merge`
--   七种里没有一种能表达「作品第一次对外可见」。审核通过（approve）不是发布——
--   设计 §6.1 明写「审核通过 ≠ 已发布」，把发布记成 approve 会让时间线上
--   「审过了」与「发出去了」再也分不开，而这正是这两个动作被拆成两个端点的理由。
--
--   也不记成 archive：成果页会把这些事件翻成中文给人看，
--   「发布到 deep」被显示成「已归档」比不显示更糟。

ALTER TABLE demo_timeline DROP CONSTRAINT ck_timeline_kind;
ALTER TABLE demo_timeline ADD CONSTRAINT ck_timeline_kind
    CHECK (kind IN ('create', 'approve', 'reject', 'archive', 'restore',
                    'slug_change', 'merge', 'publish'));

COMMENT ON COLUMN demo_timeline.kind IS
    '对外可见的动作类型：create 新建 / approve 过审 / reject 驳回 / publish 发布到渠道 / archive 下架 / restore 恢复 / slug_change 改地址 / merge 合并';
