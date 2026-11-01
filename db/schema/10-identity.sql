-- 10-identity.sql（抽取自 docs 的 DDL 块；后续以本文件为准）

-- ===== accounts  (docs/身份服务模型设计.md:592) =====
-- 账号
CREATE TABLE accounts (
    id          bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    sub         text        NOT NULL UNIQUE,          -- 对外不可变 ID：base32(128 bit)，26 字符（Q8）
    status      text        NOT NULL DEFAULT 'pending'
                            -- 取值域：标准对象 §ST-1（加值先改那里，再改本行与迁移矩阵）
                            CHECK (status IN ('pending', 'active', 'suspended', 'banned', 'deleted')),
    previous_status text    CHECK (previous_status IN ('active', 'suspended')),
    role        text        NOT NULL DEFAULT 'user',
                                                     -- 前提是两域同库（治理域 §12 第 3 步）。拆独立库（形态 C）时退回应用层校验。
    level       smallint    NOT NULL DEFAULT 0 CHECK (level BETWEEN 0 AND 6),  -- 投影镜像（事实源在成长/积分域）
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    email_hmac  text,                                   -- L2：注销时留的邮箱键控哈希；密钥不入库，核验后清空
    closed_at   timestamptz,                            -- 注销时刻（不是内容软删；账号行永不物理删除）

    -- `previous_status` 只在「当前受限」时有值，见治理域 §7.1 尾注。
    -- 注意：`suspended` 行的 `previous_status` 必须**精确等于** 'active'，不能用 `= 'active'` 直写——
    -- 它是 NULL 时结果是 NULL，而 CHECK 只在 FALSE 时拒绝，等于把 NULL 放过去（三值逻辑坑，
    -- 同治理域 §5.4 `cardinality()` 那个）。所以用 `IS NOT DISTINCT FROM`，并把 banned 的 NULL 显式挡掉。
    CONSTRAINT ck_accounts_previous_status CHECK (
        CASE status
            WHEN 'active'    THEN previous_status IS NULL
            WHEN 'suspended' THEN previous_status IS NOT DISTINCT FROM 'active'
            WHEN 'banned'    THEN previous_status IS NOT NULL
                              AND previous_status IN ('active', 'suspended')
            ELSE previous_status IS NULL
        END
    ),
    -- 注销时刻与终态同生共死：只有 `pending → deleted`（30 天未验证）与 `active → deleted`（自删）
    -- 两条边会写 closed_at，两条都把 status 置为 'deleted'。见 `标准对象 §ST-1`。
    CONSTRAINT ck_accounts_closed_status CHECK ((status = 'deleted') = (closed_at IS NOT NULL))
);

CREATE INDEX ix_accounts_status ON accounts (status) WHERE closed_at IS NULL;

CREATE UNIQUE INDEX uq_accounts_email_hmac ON accounts (email_hmac)
    WHERE email_hmac IS NOT NULL;

-- ===== credentials  (docs/身份服务模型设计.md:592) =====
-- 凭据（一个账号可多个）
CREATE TABLE credentials (
    id           bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    account_id   bigint      NOT NULL,
    kind         text        NOT NULL CHECK (kind IN ('password', 'oauth', 'passkey')),
    identifier   text        NOT NULL DEFAULT '',     -- oauth 的 provider、passkey 的 credential_id
    secret_hash  text        NOT NULL DEFAULT '',     -- password 的哈希；passkey 为空
    metadata     jsonb       NOT NULL DEFAULT '{}',   -- oauth 的 provider 数据
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz,
    UNIQUE (account_id, kind, identifier)
);

-- 上面那条只保证「同一账号不重复」，挡不住「同一个 GitHub id 绑到两个账号」——
-- 第三方身份天然全局唯一，所以单独约束（Q11 附带修的漏洞）。
CREATE UNIQUE INDEX uq_credentials_external_identifier ON credentials (kind, identifier)
    WHERE kind IN ('oauth', 'passkey');

-- ===== account_emails  (docs/身份服务模型设计.md:592) =====
-- 邮箱（可多个，一个 primary）
CREATE TABLE account_emails (
    id          bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    account_id  bigint      NOT NULL,
    email       text        NOT NULL,
    is_primary  boolean     NOT NULL DEFAULT false,
    verified_at timestamptz,
    primary_requested_at timestamptz,   -- 申请提为 primary 的时刻；48 小时后生效（§8.7）
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX uq_account_emails_email ON account_emails (lower(email));

CREATE UNIQUE INDEX uq_account_emails_primary ON account_emails (account_id)
    WHERE is_primary;

-- ===== sessions  (docs/身份服务模型设计.md:592) =====
-- 会话（支持真正的登出）
CREATE TABLE sessions (
    id          bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    account_id  bigint      NOT NULL,
    client_id   text        NOT NULL,
    user_agent  text        NOT NULL DEFAULT '',
    ip_hash     text        NOT NULL DEFAULT '',      -- 不存明文 IP；v1.5 定为**键控 HMAC-SHA256(服务端密钥, ip)**，
                                                     -- 与 `email_hmac` 同规格——裸 SHA-256 对 IPv4 只有 2³² 种输入，可枚举反推
    acr         text        NOT NULL DEFAULT 'aal1',   -- 认证保证等级（Q28）：本期恒 aal1，TOTP 上线后可为 aal2
    amr         text[]      NOT NULL DEFAULT '{pwd}',  -- 认证方法：pwd / oauth:github / totp / recovery_code
    auth_time   timestamptz NOT NULL DEFAULT now(),    -- 最后一次证明「你是你」的时刻（敏感操作按它判定，§11.5）
    created_at  timestamptz NOT NULL DEFAULT now(),
    expires_at  timestamptz NOT NULL,
    revoked_at  timestamptz,

    -- v1.5：`acr` 与 `amr` 必须自洽——aal2 不能只有密码一个因子，aal1 也不能声称用过 totp。
    CONSTRAINT ck_sessions_acr_amr CHECK (
        (acr = 'aal1' AND NOT ('totp' = ANY (amr)))
     OR (acr = 'aal2' AND 'totp' = ANY (amr))
    )
);

CREATE INDEX ix_sessions_account ON sessions (account_id) WHERE revoked_at IS NULL;

-- ===== account_marks  (docs/身份服务模型设计.md:592) =====
-- 头衔 / 装饰（展示物；注销时**全删**，见生命周期 §5.2 第 6 行）
CREATE TABLE account_marks (
    id          bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    account_id  bigint      NOT NULL,
    kind        text        NOT NULL
                            CHECK (kind IN ('title', 'frame', 'badge')),  -- 头衔 / 装饰框 / 徽章
    label       text        NOT NULL DEFAULT '',      -- 展示文案或资源 key
    granted_at  timestamptz NOT NULL DEFAULT now(),
    expires_at  timestamptz,                          -- 限时头衔；NULL = 永久
    UNIQUE (account_id, kind, label)                  -- 同一枚只能挂一次
);

CREATE INDEX ix_account_marks_account ON account_marks (account_id);

-- ===== auth_events  (docs/身份服务模型设计.md:592) =====
-- 安全日志（保留 90 天；注销后**不删**，见生命周期 §5.2 第 11 行）
-- 按月分区：保留期是「按月丢」，分区是最省事的删法（DELETE 大表会胀 WAL 与 vacuum）。
-- 分区表的主键必须含分区键，所以是 (id, created_at)，不是单列 id。
CREATE TABLE auth_events (
    id          bigint      GENERATED ALWAYS AS IDENTITY,
    account_id  bigint      NOT NULL,
    client_id   text,                                 -- 从哪个下游来的（Q12）；FK 见 §6.4
    event_type  text        NOT NULL
                            CHECK (event_type IN ('login_ok', 'login_fail', 'logout',
                                                  'password_change', 'email_change',
                                                  'session_revoke', 'status_change',
                                                  'data_export')),   -- data_export：自助导出（§12.2）
    ip_hash     text        NOT NULL DEFAULT '',      -- 同 sessions：不存明文 IP
    user_agent  text        NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (id, created_at)
) PARTITION BY RANGE (created_at);

CREATE TABLE auth_events_default PARTITION OF auth_events DEFAULT;

-- 月度分区（auth_events_p2026_11 …）由定时任务提前建，与治理域 audit_log 同一套做法。

CREATE INDEX ix_auth_events_account_time ON auth_events (account_id, created_at DESC);

CREATE INDEX ix_auth_events_retention ON auth_events (created_at);   -- 90 天清理用
CREATE INDEX ix_auth_events_ip ON auth_events (ip_hash, created_at)
    WHERE event_type = 'login_fail';                                 -- 限流滑动窗口用（§11.3）;

-- ===== profiles  (docs/身份服务模型设计.md:742) =====
CREATE TABLE profiles (
    account_id   bigint      PRIMARY KEY,
    username     text        NOT NULL                 -- 标识，不可变
                             CHECK (username ~ '^[A-Za-z0-9_-]{3,20}$'),   -- v1.5：约束落 CHECK（原先只在 §5.1 / §6.2 散文里）
    display_name text        NOT NULL DEFAULT ''      -- 昵称，可变
                             CHECK (char_length(display_name) <= 50),       -- 允许空串（= 展示时回退到 username）
    bio          text        NOT NULL DEFAULT '',
    locale       text        NOT NULL DEFAULT 'zh-CN',
    timezone     text        NOT NULL DEFAULT 'Asia/Shanghai',
    updated_at   timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX uq_profiles_username ON profiles (lower(username));

-- ===== avatars  (docs/身份服务模型设计.md:742) =====
CREATE TABLE avatars (
    account_id   bigint      PRIMARY KEY,
    url          text        NOT NULL,                -- 公开可读的稳定 URL
    content_hash text        NOT NULL,                -- sha256 前 16 位
    width        smallint    NOT NULL DEFAULT 512,
    height       smallint    NOT NULL DEFAULT 512,
    inline_data  bytea,                                -- ≤32 KiB 的内联头像（生命周期 §8.3）
    updated_at   timestamptz NOT NULL DEFAULT now()
);

-- ===== username_history  (docs/身份服务模型设计.md:742) =====
-- 用户名历史（改名后旧 URL 301）
CREATE TABLE username_history (
    id         bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    account_id bigint      NOT NULL,
    username   text        NOT NULL,
    changed_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX uq_username_history ON username_history (lower(username));

-- ===== groups  (docs/身份服务模型设计.md:789) =====
CREATE TABLE groups (
    id         bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name       text        NOT NULL UNIQUE,
    kind       text        NOT NULL DEFAULT 'custom'
                           CHECK (kind IN ('system', 'custom')),
    created_at timestamptz NOT NULL DEFAULT now()
);

-- ===== group_members  (docs/身份服务模型设计.md:789) =====
CREATE TABLE group_members (
    group_id   bigint      NOT NULL,
    account_id bigint      NOT NULL,
    granted_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (group_id, account_id)
);

-- ===== clients  (docs/身份服务模型设计.md:819) =====
-- 下游应用注册
CREATE TABLE clients (
    client_id      text        PRIMARY KEY,
    secret_hmac    text        NOT NULL DEFAULT '',   -- v1.7：只存 HMAC-SHA256(服务端密钥, client_secret)；公开客户端为空
    name           text        NOT NULL,
    kind           text        NOT NULL
                               CHECK (kind IN ('oidc', 'discourse_connect')),
    provider       text        NOT NULL DEFAULT '',   -- 'discourse' / 'demo' / …（Q7：决定调哪套 API）
    host           text        NOT NULL DEFAULT '',   -- 下游 host（不含协议），如 forum.example.com
    base_url       text        NOT NULL DEFAULT '',   -- 调下游 API 的根，如 https://forum.example.com
    redirect_uris  text[]      NOT NULL DEFAULT '{}',
    scopes         text[]      NOT NULL DEFAULT '{openid,profile,email}',
    active         boolean     NOT NULL DEFAULT true,
    created_at     timestamptz NOT NULL DEFAULT now()
);

-- ===== consents  (docs/身份服务模型设计.md:819) =====
-- 授权同意（用户授权某下游读取哪些 claim）
CREATE TABLE consents (
    id         bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    account_id bigint      NOT NULL,
    client_id  text        NOT NULL,
    scopes     text[]      NOT NULL,
    granted_at timestamptz NOT NULL DEFAULT now(),
    revoked_at timestamptz
);

-- v1.5：唯一性只在「未撤销」时成立。原来写表级 UNIQUE (account_id, client_id)，
-- 撤销后再授权必然撞唯一键（PG 18.6 实测 duplicate key）——撤销过的行不该继续占位。
CREATE UNIQUE INDEX uq_consents_active ON consents (account_id, client_id)
    WHERE revoked_at IS NULL;

-- ===== external_links  (docs/身份服务模型设计.md:819) =====
-- 外部身份映射（下游侧的 user id）
CREATE TABLE external_links (
    id          bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    account_id  bigint      NOT NULL,
    client_id   text        NOT NULL,
    external_id text        NOT NULL,                 -- 下游的 user id
    external_username text  NOT NULL DEFAULT '',      -- 下游侧用户名的快照（Q6）
    external_email    text  NOT NULL DEFAULT '',      -- 只有用户在下游授权过 email 才有；**注销 / 擦除时置 ''**（生命周期 K）
    last_synced_at    timestamptz,                    -- 最近一次把 profile 推给该下游的时间
    linked_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (account_id, client_id),
    UNIQUE (client_id, external_id)
);

-- ===== outbox_events  (docs/身份服务模型设计.md:819) =====
-- 传播事件（outbox 模式）
CREATE TABLE outbox_events (
    id           bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    account_id   bigint      NOT NULL,
    event_type   text        NOT NULL,                -- 'profile.updated' / 'avatar.updated' / 'account.deleted'
    payload      jsonb       NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    processed_at timestamptz
);

CREATE INDEX ix_outbox_pending ON outbox_events (created_at) WHERE processed_at IS NULL;

-- ===== users  (docs/身份服务模型设计.md:1160) =====
CREATE TABLE users (
    id          bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    sub         text        NOT NULL UNIQUE,          -- IdP 的 sub
    history_enabled boolean NOT NULL DEFAULT true,
    created_at  timestamptz NOT NULL DEFAULT now(),
    deleted_at  timestamptz
);

-- ===== web_sessions  (docs/身份服务模型设计.md:1324) =====
-- demo 站本地（RP 侧）。字段是 IdP 数据的快照，不是事实源
CREATE TABLE web_sessions (
    id             text PRIMARY KEY,            -- cookie 里的不透明串，不是 JWT
    account_sub    text NOT NULL,               -- IdP 的 sub
    username       text NOT NULL,
    display_name   text NOT NULL DEFAULT '',
    avatar_url     text NOT NULL DEFAULT '',
    role           text NOT NULL,               -- user | moderator | admin（快照）
    status         text NOT NULL,               -- active | suspended | banned | deleted（快照）
    verified_at    timestamptz NOT NULL,        -- 上次成功向 IdP 校验的时刻
    expires_at     timestamptz NOT NULL DEFAULT now() + interval '30 days',
    revoked_at     timestamptz,
    created_at     timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX ix_web_sessions_sub ON web_sessions (account_sub) WHERE revoked_at IS NULL;

;

;

;

;

;

;
