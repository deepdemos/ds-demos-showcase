-- 通用验证码（kernel/verification）。第一个住户是登录挑战，但机制与具体业务无关。
--
-- 为什么不叫 email_verifications：purpose 决定它服务于什么场景，渠道将来可能不止邮件
-- （短信、站内信）。表名带上渠道等于把「怎么送达」写进存储层。
CREATE TABLE verification_codes (
    id              bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    purpose         text        NOT NULL,          -- login_challenge / register_verify / email_change …
    identifier_hash text        NOT NULL,          -- HMAC(服务端密钥, 用途 + 归一化标识符)
    code_hash       text        NOT NULL,          -- HMAC(服务端密钥, 6 位码)。码空间只有 10^6，必须带密钥
    account_id      bigint,                        -- 可空：标识符可能不对应任何账号
    attempts        int         NOT NULL DEFAULT 0,
    expires_at      timestamptz NOT NULL,
    consumed_at     timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now()
);

-- 取「当前有效的那一条」用
CREATE INDEX ix_verification_codes_lookup
    ON verification_codes (purpose, identifier_hash, id DESC)
    WHERE consumed_at IS NULL;

-- 发送频率限制用（1 次/分钟、每小时 5 次）
CREATE INDEX ix_verification_codes_rate ON verification_codes (purpose, identifier_hash, created_at DESC);

-- 清理用
CREATE INDEX ix_verification_codes_time ON verification_codes (created_at);
