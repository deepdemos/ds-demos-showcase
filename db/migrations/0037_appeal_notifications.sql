-- 0037：申诉结果通知（#463 第二半）。
--
-- 事件类型仍走注册表（NTF-003）：加两行，不改 notifications 表结构。
INSERT INTO notification_kinds (key, label) VALUES
    ('appeal.approved', '作品申诉通过'),
    ('appeal.rejected', '作品申诉被驳回')
ON CONFLICT (key) DO NOTHING;
