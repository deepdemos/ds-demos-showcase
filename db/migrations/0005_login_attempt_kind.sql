-- 0005：给登录尝试表加「事件种类」。
--
-- 为什么需要它：发验证码（登录挑战 / 找回密码）也要受 IP 配额约束，否则一个 IP
-- 换着邮箱发就是一台邮件炮台。但发码不是「登录失败」，记成失败会误触发第①层
-- 的锁定，把正常用户挡在门外。所以两者同表、用 kind 分开计数。
--
-- 默认值 'login' 让迁移前的历史行自动归为登录事件，语义不变。

ALTER TABLE login_attempts
    ADD COLUMN kind text NOT NULL DEFAULT 'login';

ALTER TABLE login_attempts
    ADD CONSTRAINT ck_login_attempts_kind
    CHECK (kind IN ('login', 'email_code'));

-- 发码配额的计数索引：只数这一类，且只按 IP。
CREATE INDEX ix_login_attempts_email_code
    ON login_attempts (ip_hash, created_at DESC)
    WHERE kind = 'email_code';
