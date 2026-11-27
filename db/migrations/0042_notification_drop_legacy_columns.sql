-- 0042：删掉通知的旧作品列（#592，contract 半步）。
--
-- 0041 把通知对象泛化成「类型 + 键 + 快照标题」并回填了既有行；那之后 demo_slug /
-- demo_title 只剩回滚窗口里的双写意义，读路径（列表 / 未读计数 / 标记已读 / 读时清理）
-- 与写入路径都已经只认新列。这条把它们删掉。
--
-- **只删列，不改历史数据**：ALTER TABLE … DROP COLUMN 是元数据变更，PG 不重写表，
-- 行本身一条不动。也正因为如此，不要用「建新表 + 拷数据 + 改名」这种写法——
-- 那是唯一会让用户可见历史消失的收口方式。
--
-- 两个旧列专属的 CHECK 一并删掉（列没了它们无处可依）：
--   ck_notifications_slug   demo_slug 的 1..128（0041 已放宽成「空值放行，非空仍要 1..128」）
--   ck_notifications_title  demo_title 的 ≤ 200
-- 长度闸本身不丢：等价的两条闸已经在对象列上（ck_notifications_object_key 1..128、
-- ck_notifications_object_title ≤ 200，见 0041）。用例
-- TestIntegrationNotificationLegacyColumnsDropped 逐条撞这三条闸，证明它们没跟着旧列消失。
--
-- 两条索引（ix_notifications_time / ix_notifications_unread）都没引用旧列，不受影响。

ALTER TABLE notifications
    DROP CONSTRAINT ck_notifications_slug,
    DROP CONSTRAINT ck_notifications_title;

ALTER TABLE notifications
    DROP COLUMN demo_slug,
    DROP COLUMN demo_title;

COMMENT ON TABLE notifications IS
    '站内通知：对象用「类型 + 键 + 标题快照」存，不存外键，对象删除后仍可读（NTF-007）。';
