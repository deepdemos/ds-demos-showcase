-- 0039：提案提交频控的设置键（#525 决议）。
--
-- 为什么走设置域而不是代码常量：上限是**运营参数**（压力大时调低、活动期调高），
-- 与 `collection.max_per_account` / `auth.login_rate_account` 同一类。写进代码常量意味着
-- 每次调参都要发一次版。
--
-- 默认 10 条 / 24 小时。判据是 `created_by` + `created_at`，被驳回的提案也占配额 ——
-- 否则「提交-被驳-再提交」就是无限次。

INSERT INTO setting_keys (key, scope, value_type, default_value, description) VALUES
    ('proposal.max_per_day', 'site', 'number', '10'::jsonb, '同一账号 24 小时内最多提交几条提案（#525）')
ON CONFLICT (key, scope) DO NOTHING;
