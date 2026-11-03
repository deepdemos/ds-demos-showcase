-- 登录第②层（账号维度验证码）需要按「标识符」计数，而不是按 account_id。
--
-- 原因：`account_id` 对「账号不存在」的尝试是 NULL，而撞库最需要拦的正是无效邮箱。
-- 如果第②层只能按 account_id 计数，攻击者对着不存在的邮箱每 IP 试 5 次就绕过去了。
--
-- 存哈希不存明文：标识符是邮箱或用户名，属于个人数据。
ALTER TABLE login_attempts ADD COLUMN identifier_hash text;

-- 只索引有值的行：迁移前的历史行没有这个字段，不参与第②层计数。
CREATE INDEX ix_login_attempts_identifier
    ON login_attempts (identifier_hash, created_at DESC)
    WHERE succeeded = false AND identifier_hash IS NOT NULL;
