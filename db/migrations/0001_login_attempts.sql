-- 登录限流的计数表（身份域 §11.3）。
--
-- 为什么不复用 auth_events：那张表的 account_id 是 NOT NULL + 外键，记的是
-- 「某个人的安全历史」。而撞库最需要拦的恰恰是**不存在的账号**——那种失败没有
-- account_id，写不进 auth_events。限流计数是另一种东西：可截断、只留很短时间、
-- 不作为证据留档，所以单独一张小表，语义不互相污染。
--
-- account_id 可以为 NULL（账号不存在），且**故意不加外键**：删账号不该顺手抹掉
-- 「这个 IP 试过多少密码」的证据，那正是下一次攻击要用的。
CREATE TABLE login_attempts (
    id         bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    ip_hash    text        NOT NULL,                 -- HMAC(服务端密钥, IP)，不存明文
    account_id bigint,                               -- NULL = 该标识符没匹配到账号
    succeeded  boolean     NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now()
);

-- 账号 + IP 维度（第 1 层）
CREATE INDEX ix_login_attempts_account_ip ON login_attempts (account_id, ip_hash, created_at DESC)
    WHERE succeeded = false;

-- IP 维度（第 3 层）：不按 account_id 过滤，所以索引不能带它
CREATE INDEX ix_login_attempts_ip ON login_attempts (ip_hash, created_at DESC)
    WHERE succeeded = false;

-- 清理用：这张表只服务最近一小时，不需要长期保留。
CREATE INDEX ix_login_attempts_time ON login_attempts (created_at);
