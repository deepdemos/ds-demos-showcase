-- 50-classification.sql（抽取自 docs 的 DDL 块；后续以本文件为准）

-- ===== classifications  (docs/标签域数据结构设计.md:324) =====
CREATE TABLE classifications (
    id         bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    dimension  text        NOT NULL
                           CHECK (dimension IN ('genre', 'technique', 'subject')),
    slug       text        NOT NULL,
    label       text        NOT NULL,
    description text        NOT NULL DEFAULT '',
    parent_id      bigint,
    merged_into_id bigint,
    aliases    text[]      NOT NULL DEFAULT '{}',
    sort       int         NOT NULL DEFAULT 0,
    status     text        NOT NULL DEFAULT 'active'
                           CHECK (status IN ('active', 'deprecated')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),

    UNIQUE (dimension, slug),
    UNIQUE (dimension, id),          -- 供 join 表复合外键

    CONSTRAINT ck_classifications_shape CHECK (
        (dimension = 'genre'
            AND cardinality(aliases) = 0)
     OR (dimension = 'technique'
            AND parent_id IS NULL AND cardinality(aliases) = 0)
     OR (dimension = 'subject'
            AND parent_id IS NULL)
    )
);

CREATE INDEX ix_classifications_dim ON classifications (dimension, sort)
    WHERE status = 'active';

CREATE INDEX ix_classifications_parent ON classifications (parent_id)
    WHERE parent_id IS NOT NULL;

CREATE INDEX ix_classifications_merged ON classifications (merged_into_id)
    WHERE merged_into_id IS NOT NULL;

CREATE INDEX ix_classifications_aliases ON classifications USING gin (aliases);

-- ===== demo_classifications  (docs/标签域数据结构设计.md:393) =====
CREATE TABLE demo_classifications (
    demo_id           bigint NOT NULL,
    dimension         text   NOT NULL
                             CHECK (dimension IN ('genre', 'technique', 'subject')),
    classification_id bigint NOT NULL,
    created_at        timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (demo_id, dimension, classification_id)
);

CREATE INDEX ix_demo_cls_cls ON demo_classifications (classification_id);

-- ===== platforms  (docs/标签域数据结构设计.md:470) =====
CREATE TABLE platforms (
    id          bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    slug        text        NOT NULL UNIQUE,
    name        text        NOT NULL,
    is_harness  boolean     NOT NULL,                 -- 是不是自带 agent 循环的宿主（人工填、无默认，G17）
    vendor_id   bigint,
    url         text,
    description text        NOT NULL DEFAULT '',
    aliases     text[]      NOT NULL DEFAULT '{}',
    sort        int         NOT NULL DEFAULT 0,
    status      text        NOT NULL DEFAULT 'active'
                            CHECK (status IN ('active', 'deprecated')),
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX ix_platforms_aliases ON platforms USING gin (aliases);

-- ===== platform_forms  (docs/标签域数据结构设计.md:470) =====
CREATE INDEX ix_platforms_vendor ON platforms (vendor_id);   -- G10：平台按厂商查（对齐模型域的 ix_models_vendor）

-- 形态（多值，G17）：一个产品可以同时是 CLI / IDE 扩展 / Web
CREATE TABLE platform_forms (
    platform_id bigint NOT NULL,
    form        text   NOT NULL
                CHECK (form IN ('cli','tui','gui','web','ide','other')),
    PRIMARY KEY (platform_id, form)
);

CREATE INDEX ix_platform_forms_form ON platform_forms (form);

-- ===== demo_revision_platforms  (docs/标签域数据结构设计.md:470) =====
-- 作品↔平台是 **revision 级**（G18）：一次生成用了哪些平台，属于那一版
CREATE TABLE demo_revision_platforms (
    revision_id bigint NOT NULL,
    platform_id bigint NOT NULL,
    PRIMARY KEY (revision_id, platform_id)
);

CREATE INDEX ix_demo_rev_platforms_platform ON demo_revision_platforms (platform_id);

-- ===== platform_configs  (docs/标签域数据结构设计.md:542) =====
CREATE TABLE platform_configs (
    id          bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    platform_id bigint      NOT NULL,
    kind        text        NOT NULL
                            CHECK (kind IN ('plugin', 'preset')),   -- mcp_server 已移出（G17），见 §3.6.1
    slug        text        NOT NULL,
    name        text        NOT NULL,
    description text        NOT NULL DEFAULT '',
    url         text,
    source      text        NOT NULL DEFAULT 'manual'
                            CHECK (source IN ('manual', 'parsed', 'imported')),
    status      text        NOT NULL DEFAULT 'active'
                            CHECK (status IN ('active', 'deprecated')),
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (platform_id, kind, slug)
);

CREATE INDEX ix_platform_configs_platform ON platform_configs (platform_id, kind)
    WHERE status = 'active';

-- ===== demo_revision_platform_configs  (docs/标签域数据结构设计.md:542) =====
-- 与本版生成关联（G18）
CREATE TABLE demo_revision_platform_configs (
    revision_id bigint NOT NULL,
    config_id   bigint NOT NULL,
    PRIMARY KEY (revision_id, config_id)
);

CREATE INDEX ix_demo_rev_pcfg_cfg ON demo_revision_platform_configs (config_id);

-- ===== mcp_servers  (docs/标签域数据结构设计.md:615) =====
CREATE TABLE mcp_servers (
    id          bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    slug        text        NOT NULL UNIQUE,
    name        text        NOT NULL,
    description text        NOT NULL DEFAULT '',
    transport   text        NOT NULL
                            CHECK (transport IN ('stdio', 'sse', 'http')),
    endpoint    text        NOT NULL DEFAULT '',
    vendor_id   bigint,
    status      text        NOT NULL DEFAULT 'active'
                            CHECK (status IN ('active', 'deprecated')),
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

-- ===== demo_revision_mcp_servers  (docs/标签域数据结构设计.md:615) =====
-- 与本版生成关联（G18）：这一次生成用过哪些 MCP server
CREATE TABLE demo_revision_mcp_servers (
    revision_id   bigint NOT NULL,
    mcp_server_id bigint NOT NULL,
    PRIMARY KEY (revision_id, mcp_server_id)
);

CREATE INDEX ix_demo_rev_mcp_mcp ON demo_revision_mcp_servers (mcp_server_id);

-- ===== skills  (docs/标签域数据结构设计.md:658) =====
CREATE TABLE skills (
    id          bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    slug        text        NOT NULL UNIQUE,
    name        text        NOT NULL,
    description text        NOT NULL DEFAULT '',
    url         text,
    spec        text        NOT NULL DEFAULT 'agentskills.io'
                            CHECK (spec IN ('agentskills.io', 'other')),
    source      text        NOT NULL DEFAULT 'manual'
                            CHECK (source IN ('manual', 'parsed', 'imported')),
    status      text        NOT NULL DEFAULT 'active'
                            CHECK (status IN ('active', 'deprecated')),
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

-- ===== demo_revision_skills  (docs/标签域数据结构设计.md:658) =====
-- 与本版生成关联（G18）
CREATE TABLE demo_revision_skills (
    revision_id bigint NOT NULL,
    skill_id    bigint NOT NULL,
    PRIMARY KEY (revision_id, skill_id)
);

CREATE INDEX ix_demo_rev_skills_skill ON demo_revision_skills (skill_id);

-- ===== external_threads  (docs/标签域数据结构设计.md:725) =====
-- 泛化：subject_type + subject_id
CREATE TABLE external_threads (
    subject_type      text   NOT NULL
                             CHECK (subject_type IN (
                                 'demo', 'platform', 'platform_config',
                                 'skill', 'model', 'mcp_server', 'task'
                             )),
    subject_id        bigint NOT NULL,
    provider          text   NOT NULL,
    host              text   NOT NULL DEFAULT '',
    external_topic_id text   NOT NULL,
    external_slug     text   NOT NULL DEFAULT '',
    url               text   NOT NULL,
    post_count        int     NOT NULL DEFAULT 0,   -- 与全库一致用 int（PG 里 int = integer）
    last_posted_at    timestamptz,
    synced_at         timestamptz,
    PRIMARY KEY (subject_type, subject_id, provider),
    UNIQUE (provider, host, external_topic_id)
);

CREATE INDEX ix_ext_threads_subject ON external_threads (subject_type, subject_id);

-- ===== classification_dimensions  (docs/标签域数据结构设计.md:770) =====
CREATE TABLE classification_dimensions (
    dimension   text        PRIMARY KEY
                            CHECK (dimension IN ('genre', 'technique', 'subject')),
    label       text        NOT NULL,
    description text        NOT NULL DEFAULT '',
    mode        text        NOT NULL DEFAULT 'fixed'
                            CHECK (mode IN ('fixed', 'open')),
    tier        int         NOT NULL DEFAULT 2,
    sort        int         NOT NULL DEFAULT 0,
    status      text        NOT NULL DEFAULT 'active'
                            CHECK (status IN ('active', 'deprecated'))
);

-- 50-classification.sql 追加：论坛集成的事件收件箱（docs/论坛外置选型对比.md §7.2）
-- 归属见该文档 §7.1：external_threads 属标签域、external_events 属论坛集成（本文）。
-- 注意：external_threads 已泛化为「7 类主体」（subject_type + subject_id），本文件里的定义是唯一版本；
--       文档里旧的 (demo_id, provider) 版本已作废。

-- ===== external_events（webhook 幂等收件箱）=====
CREATE TABLE external_events (
    id           bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    provider     text        NOT NULL,
    event_id     text        NOT NULL,          -- 论坛侧事件唯一 ID
    event_type   text        NOT NULL,          -- 'post_created' / 'post_destroyed' / ...
    payload      jsonb       NOT NULL,
    processed_at timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),

    UNIQUE (provider, event_id)                  -- webhook 会重投（Discourse at-least-once），靠这个做幂等
);

CREATE INDEX ix_ext_events_pending ON external_events (provider, created_at)
    WHERE processed_at IS NULL;
