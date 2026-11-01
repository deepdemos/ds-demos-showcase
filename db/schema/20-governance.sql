-- 20-governance.sql（抽取自 docs 的 DDL 块；后续以本文件为准）

-- ===== roles  (docs/标准对象/20-枚举值域.md:125) =====
-- 治理域 §5.1（v1.13 起本表归标准对象，治理域只写「怎么用」）
CREATE TABLE roles (
    key         text        PRIMARY KEY,              -- user / moderator / admin（≤32 字符，kebab-case）
    name        text        NOT NULL,                 -- 展示名（中文）
    is_builtin  boolean     NOT NULL DEFAULT false,   -- 内置角色不可删、不可改 key
    description text        NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

INSERT INTO roles (key, name, is_builtin, description) VALUES
    ('user',      '注册用户', true, '只能操作自己拥有的资源'),
    ('moderator', '审核员',   true, '内容审核、提案审核、论坛治理'),
    ('admin',     '管理员',   true, '全部治理权限');

-- ===== permissions  (docs/标准对象/20-枚举值域.md:148) =====
-- 治理域 §5.1（v1.13 起本表归标准对象）
CREATE TABLE permissions (
    key         text        PRIMARY KEY,              -- 如 demo.review（≤64 字符）
    description text        NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now()
);

-- ===== audit_actions  (docs/标准对象/20-枚举值域.md:161) =====
-- 治理域 §5.2（v1.13 起本表归标准对象）
CREATE TABLE audit_actions (
    key         text        PRIMARY KEY,          -- review / create / delete …
    description text        NOT NULL DEFAULT '',
    is_active   boolean     NOT NULL DEFAULT true, -- 停用用这个，**不删行**
    created_at  timestamptz NOT NULL DEFAULT now()
);

INSERT INTO audit_actions (key, description) VALUES
    ('create', '新建'),            ('update', '修改'),
    ('status_set', '状态跃迁'),     ('review', '审核裁决'),
    ('merge', '合并'),             ('unmerge', '撤销合并'),
    ('attach', '建立关联'),         ('detach', '解除关联'),
    ('delete', '删除'),            ('restore', '恢复'),
    ('purge', '派生物真删'),        ('erase', '擦除内容字段'),
    ('alias_add', '加别名'),        ('alias_remove', '删别名'),
    ('attribute', '归属迁移'),      ('slug_set', '改 slug'),
    ('featured_add', '进精选'),     ('featured_remove', '出精选'),
    ('featured_order', '精选排序'),  ('featured_hero', '设为 hero'),
    ('role_set', '改角色'),         ('permission_grant', '授权'),
    ('permission_revoke', '收权'),
    ('override_content', '管理员覆写内容'),
    ('auth_event.read', '查他人安全日志');

-- ===== audit_entity_types  (docs/标准对象/20-枚举值域.md:191) =====
-- 治理域 §5.2（v1.13 起本表归标准对象）
CREATE TABLE audit_entity_types (
    key            text        PRIMARY KEY,       -- demo / account / proposal …
    description    text        NOT NULL DEFAULT '',
    table_name     text,                          -- 承载该实体的表；UNIQUE，NULL = 尚未有表
    route_template text,                          -- 后台深链模板，必须以 {id} 结尾
    is_active      boolean     NOT NULL DEFAULT true,
    created_at     timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT uq_audit_entity_table UNIQUE (table_name),
    CONSTRAINT ck_audit_entity_route CHECK (
        route_template IS NULL OR (route_template LIKE '/%' AND route_template LIKE '%{id}')
    )
);

INSERT INTO audit_entity_types (key, description, table_name, route_template) VALUES
    ('demo',            '作品',        'demos',            '/admin/demos/{id}'),
    ('demo_revision',   '作品版本',    'demo_revisions',   '/admin/revisions/{id}'),
    ('proposal',        '提案',        'proposals',        '/admin/proposals/{id}'),
    ('account',         '账号',        'accounts',         '/admin/users/{id}'),
    ('model',           '模型',        'models',           '/admin/models/{id}'),
    ('task',            '题目',        'tasks',            '/admin/tasks/{id}'),
    ('prompt',          '提示词',      'prompts',          '/admin/prompts/{id}'),
    ('prompt_library_entry', '提示词收录', 'prompt_library_entries', '/admin/prompt-library/{id}'),
    ('classification',  '分类项',      'classifications',  '/admin/classifications/{id}'),
    ('platform',        '平台',        'platforms',        '/admin/platforms/{id}'),
    ('platform_config', '平台配置',    'platform_configs', '/admin/platform-configs/{id}'),
    ('skill',           '技能',        'skills',           '/admin/skills/{id}'),
    ('mcp_server',      'MCP',         'mcp_servers',      '/admin/mcp-servers/{id}'),
    ('report',          '举报（预留）', NULL,               NULL),
    ('setting',         '站点设置',    'site_settings',    '/admin/settings/{id}');

-- ===== gc_kinds  (docs/标准对象/20-枚举值域.md:227) =====
-- 治理域 §5.3（v1.13 起本表归标准对象）
CREATE TABLE gc_kinds (
    key         text        PRIMARY KEY,          -- demo_dir / oss_object / blob …
    description text        NOT NULL DEFAULT '',
    is_active   boolean     NOT NULL DEFAULT true, -- 停用用这个，**不删行**
    created_at  timestamptz NOT NULL DEFAULT now()
);

-- ===== role_permissions  (docs/治理域设计.md:378) =====
-- 角色↔权限。两端的主键都由 标准对象 §EN-3 / §EN-4 定义
CREATE TABLE role_permissions (
    role_key       text NOT NULL,
    permission_key text NOT NULL,
    PRIMARY KEY (role_key, permission_key)
);

CREATE INDEX ix_role_permissions_permission ON role_permissions (permission_key);

-- ===== audit_log  (docs/治理域设计.md:396) =====
CREATE TABLE audit_log (
    -- 主键必须含分区键，所以拆成「列 + 表级复合主键」（见下方「分区与主键」）
    id          bigint      GENERATED ALWAYS AS IDENTITY,

    -- 谁（actor_role 是**当时的角色快照**：角色会变，布尔会失真）
    actor_type  text        NOT NULL CHECK (actor_type IN ('user', 'system')),
    actor_id    bigint,
    actor_role  text        NOT NULL,                 -- user / moderator / admin / system

    -- 做了什么（目录表 + FK：加值不用 ALTER 大表）
    action      text        NOT NULL,

    -- 对什么（多态：entity_type 决定 entity_id 指向哪张表；entity_id 本身不建外键）
    entity_type text        NOT NULL,
    entity_id   bigint      NOT NULL,

    -- 改前改后（只存关键字段的 JSON 快照：状态 / 计数 / 掩码；不存内容，也不存整行）
    before      jsonb,
    after       jsonb,
    reason      text        NOT NULL DEFAULT '' CHECK (length(reason) <= 500),

    -- 串联：同一次批量 / 同一次请求
    batch_id    uuid,
    request_id  text,

    created_at  timestamptz NOT NULL DEFAULT now(),

    -- 分区表要求：主键必须包含全部分区键（§8.4 / Q15）
    PRIMARY KEY (id, created_at)
) PARTITION BY RANGE (created_at);

CREATE INDEX ix_audit_entity  ON audit_log (entity_type, entity_id, id DESC);

CREATE INDEX ix_audit_actor   ON audit_log (actor_id, created_at DESC);

CREATE INDEX ix_audit_action  ON audit_log (action, created_at DESC);

CREATE INDEX ix_audit_batch   ON audit_log (batch_id) WHERE batch_id IS NOT NULL;

-- 分区：默认分区兜底 + 按月（维护任务提前 2 个月建，见 §8.4）
CREATE TABLE audit_log_default PARTITION OF audit_log DEFAULT;  -- 缺月不再让写入失败

CREATE TABLE audit_log_p2026_10 PARTITION OF audit_log
    FOR VALUES FROM ('2026-10-01 00:00+08') TO ('2026-11-01 00:00+08');

-- ===== gc_queue  (docs/治理域设计.md:466) =====
CREATE TABLE gc_queue (
    id           bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    kind         text        NOT NULL,
    object_key   text        NOT NULL,                  -- 目录名 / OSS key / blob hash
    entity_type  text        NOT NULL,
                                                        -- 来源对象，给 purge 审计用：demo / account …（挂同一张目录表）
    entity_id    bigint,                                -- 作品 id / 账号 id；NULL = 无对应行（孤儿）
    -- 取值域与四个值对 purge_after 的影响：标准对象 §EN-8
    reason       text        NOT NULL DEFAULT 'delete'
                             CHECK (reason IN ('delete', 'orphan', 'replace', 'erase')),
    enqueued_at  timestamptz NOT NULL DEFAULT now(),
    purge_after  timestamptz NOT NULL,                  -- 默认 enqueued_at + 30 天
    attempts     int         NOT NULL DEFAULT 0,
    last_error   text,
    purged_at    timestamptz,                           -- 真删成功
    abandoned_at timestamptz,                           -- 重试耗尽后放弃（与「删成功」区分；见 §9.3）
    UNIQUE (kind, object_key)                           -- 入队必须 upsert，见 §9.3
);

CREATE INDEX ix_gc_due ON gc_queue (purge_after) WHERE purged_at IS NULL AND abandoned_at IS NULL;

-- entity_type 挂 audit_entity_types：拼错一个字符的后果是「GC 删了文件、purge 审计写不进去、整个事务回滚」，
-- 文件已经不在盘上而队列行还在——最难查的一类。加 FK 之后，拼错在入队那一刻就失败。
CREATE INDEX ix_gc_entity ON gc_queue (entity_type, entity_id);

-- ===== review_kinds  (docs/治理域设计.md:499) =====
-- 审核队列目录（加一个队列 = INSERT 一行 + 注册一个 handler，不改路由）
CREATE TABLE review_kinds (
    key          text        PRIMARY KEY,          -- proposal / demo（report 恢复时再加）
    entity_type  text        NOT NULL,
    table_name   text        NOT NULL,             -- 承载该队列的表
    state_column text        NOT NULL,             -- 状态列名
    decisions    text[]      NOT NULL,             -- 允许的裁决集合
    is_active    boolean     NOT NULL DEFAULT true,
    created_at   timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT ck_review_decisions CHECK (
        cardinality(decisions) >= 1
        AND decisions <@ ARRAY['approve', 'reject', 'resolve', 'dismiss']::text[]
    )
);

INSERT INTO review_kinds (key, entity_type, table_name, state_column, decisions) VALUES
    ('proposal', 'proposal', 'proposals', 'status',           ARRAY['approve', 'reject']),
    ('demo',     'demo',     'demos',     'moderation_state', ARRAY['approve', 'reject']);

-- ===== reports  (docs/治理域设计.md:1324) =====
CREATE TABLE reports (
    id           bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    target_type  text        NOT NULL CHECK (target_type IN ('topic', 'reply', 'demo', 'comment')),
    target_id    bigint      NOT NULL,
    reporter_id  bigint,
    reason       text        NOT NULL DEFAULT '' CHECK (length(reason) <= 500),
    status       text        NOT NULL DEFAULT 'open'
                             CHECK (status IN ('open', 'resolved', 'dismissed')),
    handled_by   bigint,
    handled_at   timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (target_type, target_id, reporter_id)      -- 同一个人对同一目标只报一次
);

CREATE INDEX ix_reports_open ON reports (status, created_at) WHERE status = 'open';

;

;

;

;

;

;
-- 20-governance.sql 追加：权限目录种子 + 内置角色绑定 + GC 对象类型种子
-- 来源：docs/治理域设计.md §4.3（20 个权限，admin 全绑、moderator 五条）、docs/标准对象/20-枚举值域.md §EN-7（gc_kinds 3 条）
-- 注意：这张表的「说明」列与文档 §4.3 保持一致；改一处必须改两处（contract 门禁只校验键与绑定，不校验文案）。

-- ===== permissions 种子（20 条）=====
INSERT INTO permissions (key, description) VALUES
    ('demo.review',          '审核（approve / reject）'),
    ('demo.curate',          '渠道通行证 sites / lang'),
    ('demo.featured',        '精选池增删 / 排序 / hero'),
    ('demo.delete',          '删除作品（软删）'),
    ('content.erase',        '强制擦除任一内容行的字段（四张内容表通用）'),
    ('demo.rebuild',         '重建内容（管理员修正 → 生成新 revision）'),
    ('proposal.review',      '审核提案（approve 触发执行）'),
    ('forum.moderate',       '主题/回复隐藏、置顶、锁定'),
    ('report.handle',        '处理举报（当前无队列，预留）'),
    ('entity.read',          '读管理端实体列表（含非公开状态）'),
    ('entity.edit',          '改实体字段（白名单）'),
    ('entity.merge',         '合并 / 撤销合并'),
    ('entity.status',        '实体状态跃迁（candidate/active/…）'),
    ('classification.edit',  '分类词表增删改、分组'),
    ('attribution.write',    '归属工作台（把作品从兜底位迁到真实型号）'),
    ('user.read',            '读账号列表（含状态）'),
    ('user.status',          '改账号状态（suspend / ban / restore）'),
    ('user.role',            '改账号角色'),
    ('audit.read',           '读审计'),
    ('ops.run',              '巡检、OSS 同步、GC 触发、类型细分预览'),
    ('setting.write',        '改站点参数（设置域，v1.14 随该域落地）');

-- ===== role_permissions：admin 全绑，moderator 五条 =====
INSERT INTO role_permissions (role_key, permission_key)
SELECT 'admin', key FROM permissions;

INSERT INTO role_permissions (role_key, permission_key) VALUES
    ('moderator', 'demo.review'),
    ('moderator', 'proposal.review'),
    ('moderator', 'forum.moderate'),
    ('moderator', 'report.handle'),
    ('moderator', 'entity.read');

-- ===== gc_kinds 种子（3 条；加一种派生物 = 加一行）=====
INSERT INTO gc_kinds (key, description) VALUES
    ('demo_dir',   '作品目录前缀'),
    ('oss_object', 'OSS 上的单个对象'),
    ('blob',       '内容寻址的 blob');
