-- 15-settings.sql —— 设置域（设计见 docs/设置域设计.md）
--
-- 三张表：参数注册表 setting_keys + 两张值表 site_settings / user_preferences。
-- 为什么是三张不是两张：白名单必须由数据库强制，不能放应用层常量。
-- 旧栈把白名单写在 Python 常量里，注释自认 tag / user / setting 各漏过一次
-- （治理域设计 §4.4）；SET-010 要求「未登记的 key 写不进去」，
-- 应用层 if 判断满足不了这条，外键才满足。
--
-- 三张表都属「主数据表」（DDL 约定 §7.1）：不要 deleted_at / erased_at，
-- 退役靠 setting_keys.is_active = false，值行永不物理删除（标准对象 §ID-5 通则①）。

-- ===== setting_keys：参数注册表（白名单 + 类型 + 默认值） =====
-- 加一项参数 = INSERT 一行，不改表结构、不改接口形状（SET-001）。
-- 主键是 (key, scope) 而不是 key：同一个语义允许在站点级与用户级各有一行
-- （例：notify.retention_days 既是站点默认，也允许用户覆盖）。
-- 键名格式「域.键」：域段是「这个参数影响哪个域」，不是设置域自己。
CREATE TABLE setting_keys (
    key           text        NOT NULL,
    scope         text        NOT NULL CHECK (scope IN ('site', 'user')),
    value_type    text        NOT NULL CHECK (value_type IN ('boolean', 'number', 'string', 'json')),
    default_value jsonb       NOT NULL,
    description   text        NOT NULL DEFAULT '',
    is_active     boolean     NOT NULL DEFAULT true,
    created_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (key, scope),
    -- 给两张值表的复合外键用：值行带着 value_type，改类型必须同步改值行
    CONSTRAINT uq_setting_keys_type UNIQUE (key, scope, value_type),
    CONSTRAINT ck_setting_keys_form CHECK (key ~ '^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$'),
    -- 默认值的 JSON 类型必须与声明的 value_type 一致；null 不是合法默认值
    CONSTRAINT ck_setting_keys_default CHECK (
        CASE value_type
            WHEN 'boolean' THEN jsonb_typeof(default_value) = 'boolean'
            WHEN 'number'  THEN jsonb_typeof(default_value) = 'number'
            WHEN 'string'  THEN jsonb_typeof(default_value) = 'string'
            ELSE               jsonb_typeof(default_value) IN ('object', 'array')
        END
    )
);

-- ===== site_settings：站点级取值（一键一行） =====
-- 缺行 = 用 setting_keys.default_value，不预置、不回填（D5）。
-- scope 是一个常量列，只为让复合外键把「站点参数」钉死在 scope='site' 的注册行上（D4）。
CREATE TABLE site_settings (
    id          bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    key         text        NOT NULL,
    scope       text        NOT NULL DEFAULT 'site' CHECK (scope = 'site'),
    value_type  text        NOT NULL,
    value       jsonb       NOT NULL,
    updated_at  timestamptz NOT NULL DEFAULT now(),
    updated_by  bigint,
    UNIQUE (key),
    CONSTRAINT ck_site_settings_value CHECK (
        CASE value_type
            WHEN 'boolean' THEN jsonb_typeof(value) = 'boolean'
            WHEN 'number'  THEN jsonb_typeof(value) = 'number'
            WHEN 'string'  THEN jsonb_typeof(value) = 'string'
            ELSE               jsonb_typeof(value) IN ('object', 'array')
        END
    )
);

-- ===== user_preferences：用户级取值（一人一键一行） =====
-- 没有 updated_by：这一行只可能由本人写（SET-009），端点不接受目标 account_id。
-- 不落审计（D9）：只影响本人可见结果；「浏览历史开关」本身是隐私声明，
-- 给它再记一份审计等于把隐私选择变成可追溯的操作记录。
CREATE TABLE user_preferences (
    account_id  bigint      NOT NULL,
    key         text        NOT NULL,
    scope       text        NOT NULL DEFAULT 'user' CHECK (scope = 'user'),
    value_type  text        NOT NULL,
    value       jsonb       NOT NULL,
    updated_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (account_id, key),
    CONSTRAINT ck_user_preferences_value CHECK (
        CASE value_type
            WHEN 'boolean' THEN jsonb_typeof(value) = 'boolean'
            WHEN 'number'  THEN jsonb_typeof(value) = 'number'
            WHEN 'string'  THEN jsonb_typeof(value) = 'string'
            ELSE               jsonb_typeof(value) IN ('object', 'array')
        END
    )
);

-- ===== 首批参数（8 行；加参数 = 这里加一行，不改表结构、不改接口） =====
-- 默认值只写在这里一次。代码里不许再出现第二份默认值（IDN-005 / NTF-008 的反面）。
-- 改默认值 = 改注册表行 + 一条 migration；不改已存在的值行（值行是「管理员改过」的证据）。
INSERT INTO setting_keys (key, scope, value_type, default_value, description) VALUES
    ('demo.auto_approve',           'site', 'boolean', 'false'::jsonb, '全局自动上架：新提交的作品跳过审核直接上架（SET-001）'),
    ('demo.anonymous_auto_approve', 'site', 'boolean', 'false'::jsonb, '匿名（未登录）提交的作品是否跳过审核直接上架（SET-001）'),
    ('auth.login_rate_account',     'site', 'number',  '30'::jsonb,    '5 分钟内同一账号名允许的登录失败次数（IDN-005）'),
    ('auth.login_rate_network',     'site', 'number',  '60'::jsonb,    '5 分钟内同一网络允许的登录失败次数（IDN-005）'),
    ('notify.retention_days',       'site', 'number',  '90'::jsonb,    '通知默认保留天数（NTF-008）'),
    ('notify.retention_days',       'user', 'number',  '90'::jsonb,    '用户自己的通知保留天数，只允许配得比站点值更短（NTF-008）'),
    ('history.record',              'user', 'boolean', 'true'::jsonb,  '浏览是否形成账号历史（SET-007 / SET-008）'),
    ('collection.public_default',   'user', 'boolean', 'false'::jsonb, '新建自定义收藏夹是否默认公开；默认夹永远私密，不受此键影响（COL-002）');
