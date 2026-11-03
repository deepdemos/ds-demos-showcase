-- 0007：users 投影表落地。
--
-- 这张表建了很久但从来没有行：全仓没有一处 INSERT，7 条指向它的外键的列
-- 永远是 NULL（实体域的 created_by / reviewed_by 等），demo 域的 author_id
-- 又写着 accounts.id，语义与文档 §3.1「FK→users」不一致。
--
-- 本迁移做两件事：
--
--   1. 给已有 accounts 补 users 行（按 sub 对齐，幂等）。当前库里是 0 行，
--      但迁移必须能重放——真跑在有数据的环境里时，这条是唯一的数据来源。
--
--   2. 删掉 users.history_enabled。它是「浏览是否形成账号历史」的旧存法，
--      而设置域 §4 已经把同一件事登记成用户级设置键 history.record
--      （user_preferences 一行）。留着两处 = 制造第二个事实源，
--      而「偏好该存哪」这条判据（设置域 §4.1 J6）已经有明确答案：
--      后端真的要读来做判定的进 setting_keys。历史是否记录正是这种。

INSERT INTO users (sub)
SELECT a.sub FROM accounts a
ON CONFLICT (sub) DO NOTHING;

ALTER TABLE users DROP COLUMN history_enabled;
