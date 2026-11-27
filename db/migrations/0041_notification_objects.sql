-- 0041：通知对象泛化（#591，expand 半步）。
--
-- 通知对象从「作品」泛化为「对象类型 + 对象键 + 对象快照标题」，模型 / 提案这类事件
-- 因此也能通知（NTF-003：事件类型由注册表驱动；NTF-007：对象存快照，不存外键）。
--
-- **只加不删**：旧的 demo_slug / demo_title 是用户可见的历史，改名加删列放在同一条迁移里
-- 一旦回滚就丢数据。它们保留一版——作品类事件仍双写（回滚窗口），读路径已全部改看新列，
-- 由下一条迁移删除（#592）。
--
-- demo_slug 的 NOT NULL 必须放开：非作品对象（模型 / 提案）没有作品 slug，否则写不进去。
-- 长度闸保留，改成「空值放行，非空仍要 1..128」。

ALTER TABLE notifications
    ADD COLUMN object_type  text NOT NULL DEFAULT 'demo',
    ADD COLUMN object_key   text NOT NULL DEFAULT '',
    ADD COLUMN object_title text NOT NULL DEFAULT '';

-- 回填：既有行全是作品对象，三列直接取旧列的值（与旧列逐行一致）。
UPDATE notifications
SET object_type  = 'demo',
    object_key   = demo_slug,
    object_title = demo_title;

-- 三个新列不允许缺省：漏写要当场报错，而不是静默落一条对象为空的空壳通知。
ALTER TABLE notifications
    ALTER COLUMN object_type  DROP DEFAULT,
    ALTER COLUMN object_key   DROP DEFAULT,
    ALTER COLUMN object_title DROP DEFAULT,
    ALTER COLUMN demo_slug    DROP NOT NULL;

ALTER TABLE notifications DROP CONSTRAINT ck_notifications_slug;
ALTER TABLE notifications ADD CONSTRAINT ck_notifications_slug
    CHECK (demo_slug IS NULL OR char_length(demo_slug) BETWEEN 1 AND 128);

ALTER TABLE notifications
    ADD CONSTRAINT ck_notifications_object_type
        CHECK (object_type ~ '^[a-z][a-z0-9_]{1,31}$'),
    ADD CONSTRAINT ck_notifications_object_key
        CHECK (char_length(object_key) BETWEEN 1 AND 128),
    ADD CONSTRAINT ck_notifications_object_title
        CHECK (char_length(object_title) <= 200);

COMMENT ON COLUMN notifications.demo_slug IS
    '旧列：作品 slug 快照。读路径已不再依赖它，只由作品类事件在回滚窗口里双写。下一条迁移删除（#592）。';
COMMENT ON COLUMN notifications.demo_title IS
    '旧列：作品标题快照。读路径已不再依赖它，只由作品类事件在回滚窗口里双写。下一条迁移删除（#592）。';
COMMENT ON COLUMN notifications.object_type IS
    '对象类型键（demo / model / proposal），取值与事件类型注册表 notification_kinds.object_type 一致。';
COMMENT ON COLUMN notifications.object_key IS
    '对象键：作品与模型是 slug，提案是提案 id 的十进制串。';
COMMENT ON COLUMN notifications.object_title IS
    '事件发生时的对象标题快照；对象删除后仍能显示（NTF-007）。';

-- 两个模型提案事件（本票只插行；#593 / #595 负责发，不再插行）。
-- 通过通知的对象是模型（那时模型已建出、有链接可点），驳回通知的对象是提案（那时没有模型）：
-- 两者对象类型不同是正常形态，不为统一而硬凑。
INSERT INTO notification_kinds (key, label, object_type) VALUES
    ('model.proposal.approved', '模型提案通过', 'model'),
    ('model.proposal.rejected', '模型提案驳回', 'proposal')
ON CONFLICT (key) DO NOTHING;
