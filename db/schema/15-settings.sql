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

-- 什么进这张注册表、什么不进：见 docs/运行手册-可变常量.md 的判据一节。
-- 一句话：改了立刻生效、不需要 migration、不需要重算、不会让旧数据自相矛盾的，进这里；
-- 否则进运行手册（登记「唯一改动点」），不进设置。

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

-- ===== 首批参数（19 行：site 16 + user 3；加参数 = 这里加一行，不改表结构、不改接口） =====
-- 默认值只写在这里一次。代码里不许再出现第二份默认值（IDN-005 / NTF-008 的反面）。
-- 改默认值 = 改注册表行 + 一条 migration；不改已存在的值行（值行是「管理员改过」的证据）。
--
-- 这批的来历（v1.1，2026-11-01）：前 8 行来自 SET-001 / IDN-005 / NTF-008 / SET-007；
-- 后 11 行来自一次全仓盘点（docs/运行手册-可变常量.md），把散在各域正文里的
-- 运营性上限收进来，让 GEN-012「上限只在一处」第一次真的成立。
-- 注意：结构性上限（封面内联 32 KiB、username 3–20 等）不进这里，它们要 migration，
-- 登记在运行手册的「结构性上限」一节。
INSERT INTO setting_keys (key, scope, value_type, default_value, description) VALUES
    -- ① 审核与上架（SET-001）
    ('demo.auto_approve',             'site', 'boolean', 'false'::jsonb, '全局自动上架：新提交的作品跳过审核直接上架（SET-001）'),
    ('demo.anonymous_auto_approve',   'site', 'boolean', 'false'::jsonb, '匿名（未登录）提交的作品是否跳过审核直接上架（SET-001）'),
    -- ② 上传与封面（DEMO-003 / DEMO-005）
    ('demo.upload_package_mib',       'site', 'number',  '200'::jsonb,   '文件包单包大小上限 MiB（DEMO-003；超限转管理员审批，不硬拒）'),
    ('demo.upload_extracted_mib',     'site', 'number',  '500'::jsonb,   '文件包累计解压大小上限 MiB（DEMO-003）'),
    ('demo.cover_max_megapixels',     'site', 'number',  '40'::jsonb,    '封面图最大像素数，单位百万（DEMO-005）'),
    -- ③ 登录限流（IDN-005）
    ('auth.login_rate_account',       'site', 'number',  '30'::jsonb,    '5 分钟内同一账号名允许的登录失败次数（IDN-005）'),
    ('auth.login_rate_network',       'site', 'number',  '60'::jsonb,    '5 分钟内同一网络允许的登录失败次数（IDN-005）'),
    -- ④ 收藏与历史容量（COL-004 / HIST-006）
    ('collection.max_per_account',    'site', 'number',  '20'::jsonb,    '每个账号可建的收藏夹数量上限（COL-004）'),
    ('collection.max_items',          'site', 'number',  '500'::jsonb,   '每个收藏夹可收藏的作品数量上限（COL-004）'),
    ('history.max_entries_account',   'site', 'number',  '200'::jsonb,   '账号浏览历史保留条数上限（HIST-006）'),
    ('history.max_entries_local',     'site', 'number',  '100'::jsonb,   '本机浏览历史保留条数上限，前端读取（HIST-006）'),
    -- ⑤ 生命周期（L1）
    ('lifecycle.restore_window_days', 'site', 'number',  '30'::jsonb,    '软删除后可恢复的天数；只影响后续恢复判定，不回溯（生命周期 §13 L1）'),
    -- ⑥ 外部模型目录同步（模型域 §6.3）
    ('modelsdev.sync_enabled',        'site', 'boolean', 'false'::jsonb, '是否开启 models.dev 定时刷新（模型域 §6.3）'),
    ('modelsdev.sync_interval_hours', 'site', 'number',  '24'::jsonb,    'models.dev 定时刷新间隔小时数（模型域 §6.3）'),
    ('modelsdev.search_official_only','site', 'boolean', 'true'::jsonb,  '模型导入页默认只显示官方来源（模型域 §6.3）'),
    -- ⑦ 通知（NTF-008）
    ('notify.retention_days',         'site', 'number',  '90'::jsonb,    '通知默认保留天数（NTF-008）'),
    -- ⑧ 用户级偏好（SET-007 / SET-008 / NTF-008）
    ('notify.retention_days',         'user', 'number',  '90'::jsonb,    '用户自己的通知保留天数，只允许配得比站点值更短（NTF-008）'),
    ('history.record',                'user', 'boolean', 'true'::jsonb,  '浏览是否形成账号历史（SET-007 / SET-008）'),
    ('collection.public_default',     'user', 'boolean', 'false'::jsonb, '新建自定义收藏夹是否默认公开；默认夹永远私密，不受此键影响（COL-002）');
