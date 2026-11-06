-- 0021：把 `modelsdev.sync_enabled` 的默认值从 false 改成 true。
--
-- 这个键是随设置域一起登记进基线的，当时模型目录同步**还没实现**——默认 false
-- 的意思是「没有可同步的东西，别开」。同步落地之后（PR 让 `POST /admin/models/sync`
-- 与进程内定时器都变成真事），默认 false 就变成了另一句话：「装好了但永远不动」，
-- 与「都从上游同步了，不自动化干嘛」的口径相反。
--
-- 只改默认值，不动已写过的 `site_settings` 行：运营显式关掉过就保持关掉——
-- 那是一个决定，不该被迁移覆盖。`GetSiteSettingBoolean` 走的是
-- `COALESCE(s.value, k.default_value)`，所以没有行时就会取到这个新默认。

UPDATE setting_keys
SET default_value = 'true'::jsonb
WHERE key = 'modelsdev.sync_enabled';
