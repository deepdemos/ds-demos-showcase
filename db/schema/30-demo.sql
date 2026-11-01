-- 30-demo.sql —— Demo 域（作品 / 版本 / 清单 / blob / 渠道 / 统计 / 评分 / 时间线 / slug 历史）
-- 依据：docs/Demo域设计.md §3.1–§3.10 字段表、§9 不变量；标准对象 §ST-4 / §ST-5 / §EN-1 / §TS-1
-- 交叉域外键（→ users / prompts / tasks）见 90-late-fk.sql

-- ===== demos =====
CREATE TABLE demos (
    id                  bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    slug                text        NOT NULL UNIQUE,
    title               text,
    description         text,
    cover_url           text,
    cover_thumb_url     text,
    cover_inline        bytea,
    author_id           bigint,
    guest_name          text,
    current_revision_id bigint,
    moderation_state    text        NOT NULL DEFAULT 'pending',
    lifecycle_state     text        NOT NULL DEFAULT 'draft',
    idempotency_key     text,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    deleted_at          timestamptz,
    erased_at           timestamptz,
    delete_reason       text        NOT NULL DEFAULT '',

    CONSTRAINT ck_demos_slug_len     CHECK (length(slug) BETWEEN 1 AND 128),
    CONSTRAINT ck_demos_title_len    CHECK (title IS NULL OR length(title) <= 200),
    CONSTRAINT ck_demos_cover_len    CHECK (cover_url IS NULL OR length(cover_url) <= 500),
    CONSTRAINT ck_demos_thumb_len    CHECK (cover_thumb_url IS NULL OR length(cover_thumb_url) <= 500),
    CONSTRAINT ck_demos_cover_inline CHECK (cover_inline IS NULL OR octet_length(cover_inline) <= 32768),
    CONSTRAINT ck_demos_guest_len    CHECK (guest_name IS NULL OR length(guest_name) <= 64),
    CONSTRAINT ck_demos_idem_len     CHECK (idempotency_key IS NULL OR length(idempotency_key) <= 128),
    CONSTRAINT ck_demos_reason_len   CHECK (length(delete_reason) <= 200),
    CONSTRAINT ck_demos_moderation   CHECK (moderation_state IN ('pending', 'approved', 'rejected')),
    CONSTRAINT ck_demos_lifecycle    CHECK (lifecycle_state IN ('draft', 'published', 'archived', 'removed')),
    CONSTRAINT ck_demos_time_order   CHECK (updated_at >= created_at
                                            AND (deleted_at IS NULL OR deleted_at >= created_at)
                                            AND (erased_at IS NULL OR (deleted_at IS NOT NULL AND erased_at >= deleted_at))),
    CONSTRAINT ck_demos_erased       CHECK (erased_at IS NULL
                                            OR (title IS NULL AND description IS NULL
                                                AND cover_url IS NULL AND cover_thumb_url IS NULL
                                                AND cover_inline IS NULL))
);

CREATE UNIQUE INDEX uq_demos_idempotency_key ON demos (idempotency_key)
    WHERE idempotency_key IS NOT NULL;

CREATE INDEX ix_demos_visible ON demos (moderation_state, lifecycle_state)
    WHERE deleted_at IS NULL;

CREATE INDEX ix_demos_author ON demos (author_id) WHERE author_id IS NOT NULL;

-- ===== demo_revisions =====
CREATE TABLE demo_revisions (
    id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    demo_id       bigint      NOT NULL,
    rev_no        int         NOT NULL,
    title         text,
    description   text,
    cover_url     text,
    cover_inline  bytea,
    demo_type     text        NOT NULL DEFAULT 'web',
    single_file   text,
    external_url  text,
    entry_path    text,
    manifest_hash text,
    file_count    bigint,
    total_bytes   bigint,
    prompt        text,
    prompt_id     bigint,
    video_url     text,
    model_hint    text,
    gen_rounds    int,
    gen_minutes   int,
    source        text        NOT NULL DEFAULT 'upload',
    created_by    bigint,
    created_at    timestamptz NOT NULL DEFAULT now(),
    deleted_at    timestamptz,
    erased_at     timestamptz,

    CONSTRAINT uq_demo_revisions_no    UNIQUE (demo_id, rev_no),
    CONSTRAINT ck_rev_rev_no           CHECK (rev_no >= 1),
    CONSTRAINT ck_rev_title_len        CHECK (title IS NULL OR length(title) <= 200),
    CONSTRAINT ck_rev_cover_len        CHECK (cover_url IS NULL OR length(cover_url) <= 500),
    CONSTRAINT ck_rev_cover_inline     CHECK (cover_inline IS NULL OR octet_length(cover_inline) <= 32768),
    CONSTRAINT ck_rev_demo_type        CHECK (demo_type IN ('web', 'zip', 'link', 'image')),
    CONSTRAINT ck_rev_single_file      CHECK (single_file IS NULL OR single_file IN ('html', 'svg')),
    CONSTRAINT ck_rev_external_url_len CHECK (external_url IS NULL OR length(external_url) <= 2000),
    CONSTRAINT ck_rev_entry_path_len   CHECK (entry_path IS NULL OR length(entry_path) <= 500),
    CONSTRAINT ck_rev_manifest_len     CHECK (manifest_hash IS NULL OR length(manifest_hash) = 64),
    CONSTRAINT ck_rev_file_count       CHECK (file_count IS NULL OR file_count >= 0),
    CONSTRAINT ck_rev_total_bytes      CHECK (total_bytes IS NULL OR total_bytes >= 0),
    CONSTRAINT ck_rev_video_url_len    CHECK (video_url IS NULL OR length(video_url) <= 2000),
    CONSTRAINT ck_rev_model_hint_len   CHECK (model_hint IS NULL OR length(model_hint) <= 500),
    CONSTRAINT ck_rev_source           CHECK (source IN ('upload', 'url_import', 'rollback')),
    CONSTRAINT ck_rev_time_order       CHECK (erased_at IS NULL
                                              OR (deleted_at IS NOT NULL AND erased_at >= deleted_at)),
    CONSTRAINT ck_rev_erased_content   CHECK (erased_at IS NULL
                                              OR (title IS NULL AND description IS NULL
                                                  AND cover_url IS NULL AND cover_inline IS NULL
                                                  AND entry_path IS NULL AND manifest_hash IS NULL
                                                  AND file_count IS NULL AND total_bytes IS NULL
                                                  AND prompt IS NULL AND prompt_id IS NULL
                                                  AND video_url IS NULL AND model_hint IS NULL))
);

CREATE INDEX ix_demo_revisions_demo ON demo_revisions (demo_id, rev_no DESC);

-- ===== blobs（内容寻址；先于 demo_revision_files 建）=====
CREATE TABLE blobs (
    sha256      text        PRIMARY KEY,
    size        bigint      NOT NULL,
    mime        text        NOT NULL,
    storage_key text        NOT NULL,
    backend     text        NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    last_ref_at timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT ck_blobs_sha256   CHECK (length(sha256) = 64),
    CONSTRAINT ck_blobs_size     CHECK (size >= 0),
    CONSTRAINT ck_blobs_mime_len CHECK (length(mime) <= 128),
    CONSTRAINT ck_blobs_key_len  CHECK (length(storage_key) <= 500),
    CONSTRAINT ck_blobs_backend  CHECK (backend IN ('local', 'oss'))
);

-- ===== demo_revision_files（清单）=====
CREATE TABLE demo_revision_files (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    revision_id bigint NOT NULL,
    path        text   NOT NULL,
    blob_hash   text   NOT NULL,
    size        bigint NOT NULL,
    mime        text   NOT NULL,

    CONSTRAINT uq_manifest_path UNIQUE (revision_id, path),
    CONSTRAINT ck_file_path_len CHECK (length(path) BETWEEN 1 AND 500),
    CONSTRAINT ck_file_size     CHECK (size >= 0),
    CONSTRAINT ck_file_mime_len CHECK (length(mime) <= 128)
);

CREATE INDEX ix_manifest_blob ON demo_revision_files (blob_hash);

-- ===== demo_channels =====
CREATE TABLE demo_channels (
    id             bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    demo_id        bigint      NOT NULL,
    channel        text        NOT NULL,
    visibility     text        NOT NULL DEFAULT 'listed',
    lang           text        NOT NULL DEFAULT 'zh',
    featured       boolean     NOT NULL DEFAULT false,
    featured_order int,
    published_at   timestamptz,
    curated_by     bigint,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT uq_channel        UNIQUE (demo_id, channel),
    CONSTRAINT ck_channel_name   CHECK (length(channel) BETWEEN 1 AND 32),
    CONSTRAINT ck_channel_vis    CHECK (visibility IN ('listed', 'unlisted', 'hidden')),
    CONSTRAINT ck_channel_lang   CHECK (length(lang) BETWEEN 1 AND 8),
    CONSTRAINT ck_channel_order  CHECK (featured_order IS NULL OR featured_order >= 1),
    CONSTRAINT ck_channel_time   CHECK (updated_at >= created_at
                                        AND (published_at IS NULL OR published_at >= created_at))
);

CREATE INDEX ix_channel_listing ON demo_channels (channel, visibility, featured_order);

-- ===== demo_stats（可重建）=====
CREATE TABLE demo_stats (
    demo_id        bigint       PRIMARY KEY,
    view_count     bigint       NOT NULL DEFAULT 0,
    download_count bigint       NOT NULL DEFAULT 0,
    rating_sum     bigint       NOT NULL DEFAULT 0,
    rating_count   bigint       NOT NULL DEFAULT 0,
    rating_avg     numeric(3,2) NOT NULL DEFAULT 0,
    rating_god     bigint       NOT NULL DEFAULT 0,
    rating_ghost   bigint       NOT NULL DEFAULT 0,
    updated_at     timestamptz  NOT NULL DEFAULT now(),

    CONSTRAINT ck_stat_counts CHECK (view_count >= 0 AND download_count >= 0
                                     AND rating_sum >= 0 AND rating_count >= 0
                                     AND rating_god >= 0 AND rating_ghost >= 0),
    CONSTRAINT ck_stat_avg    CHECK (rating_avg >= 0 AND rating_avg <= 5)
);

-- ===== demo_ratings（同一人一票，改分是 UPDATE，撤销是 DELETE）=====
CREATE TABLE demo_ratings (
    id         bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    demo_id    bigint      NOT NULL,
    user_id    bigint,
    rater_key  text        NOT NULL,
    score      int         NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT uq_rating_per_rater UNIQUE (demo_id, rater_key),
    CONSTRAINT ck_rating_score   CHECK (score BETWEEN 1 AND 5),
    CONSTRAINT ck_rating_key_len CHECK (length(rater_key) BETWEEN 1 AND 128),
    CONSTRAINT ck_rating_time    CHECK (updated_at >= created_at)
);

CREATE INDEX ix_ratings_demo ON demo_ratings (demo_id);

-- ===== demo_timeline（只记事件，不记版本）=====
CREATE TABLE demo_timeline (
    id         bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    demo_id    bigint      NOT NULL,
    kind       text        NOT NULL,
    message    text,
    actor_id   bigint,
    created_at timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT ck_timeline_kind    CHECK (kind IN ('create', 'approve', 'reject', 'archive',
                                                   'restore', 'slug_change', 'merge')),
    CONSTRAINT ck_timeline_msg_len CHECK (message IS NULL OR length(message) <= 500)
);

CREATE INDEX ix_timeline_demo ON demo_timeline (demo_id, created_at DESC);

-- ===== demo_slug_history（slug 永久性，旧 slug 301）=====
CREATE TABLE demo_slug_history (
    slug       text        PRIMARY KEY,
    demo_id    bigint      NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT ck_slug_hist_len CHECK (length(slug) BETWEEN 1 AND 128)
);

-- ===== demo_task_overrides（人工改题归属；任务本身属实体域）=====
CREATE TABLE demo_task_overrides (
    demo_id    bigint      NOT NULL,
    task_id    bigint      NOT NULL,
    reason     text        NOT NULL DEFAULT '',
    created_by bigint,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (demo_id, task_id)
);

;

;

;

;

-- 30-demo.sql 追加：Demo 运行能力契约（docs/Demo运行能力契约.md §3）
-- 能力目录 + 授予表。AUTHORITY：db/schema/30-demo.sql

-- ===== demo_capabilities（能力目录；退役用 is_active=false，禁删行）=====
CREATE TABLE demo_capabilities (
    key            text PRIMARY KEY,                 -- '<resource>.<action>'
    summary        text NOT NULL,                    -- 中文一句话，前端与文档直接读
    kind           text NOT NULL CHECK (kind IN ('read', 'write', 'net', 'realtime')),
    needs_review   boolean NOT NULL DEFAULT false,   -- 开这个能力要不要人工审核
    quota_default  jsonb NOT NULL DEFAULT '{}'::jsonb,
    is_active      boolean NOT NULL DEFAULT true,
    doc_url        text,
    created_at     timestamptz NOT NULL DEFAULT now(),

    -- 通常是 '<resource>.<action>'；也允许单段（如 'realtime' 这类整块能力）
    CONSTRAINT ck_capabilities_key_shape CHECK (key ~ '^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)?$'),
    CONSTRAINT ck_capabilities_summary_len CHECK (length(summary) BETWEEN 1 AND 200)
);

-- ===== demo_grants（每个 demo 被授予的能力）=====
CREATE TABLE demo_grants (
    demo_id     bigint NOT NULL,
    capability  text   NOT NULL,
    granted_at  timestamptz NOT NULL DEFAULT now(),
    granted_by  bigint,
    revoked_at  timestamptz,

    PRIMARY KEY (demo_id, capability)
);

CREATE INDEX ix_demo_grants_capability ON demo_grants (capability) WHERE revoked_at IS NULL;

-- 种子（Step 1 只有一行真正被用；其余先建行，免得上线时补表）
INSERT INTO demo_capabilities (key, summary, kind, needs_review, quota_default, doc_url) VALUES
    ('identity.read',     '读取当前访客的公开身份（sub／用户名／昵称／头像）', 'read',     false, '{}'::jsonb,                          '/docs/Demo运行能力契约.md#4'),
    ('leaderboard.read',  '读取本 demo 的排行榜',                            'read',     false, '{}'::jsonb,                          '/docs/Demo运行能力契约.md#4'),
    ('leaderboard.write', '向本 demo 的排行榜提交成绩',                       'write',    false, '{"per_minute": 6, "per_day": 200}'::jsonb, '/docs/Demo运行能力契约.md#4'),
    ('storage.read',      '读取本 demo 自己的数据存储',                       'read',     false, '{}'::jsonb,                          NULL),
    ('storage.write',     '写入本 demo 自己的数据存储',                       'write',    false, '{"per_minute": 60}'::jsonb,          NULL),
    ('net.proxy',         '通过平台代理请求外部 URL',                         'net',      true,  '{"per_minute": 30}'::jsonb,          NULL),
    ('realtime',          '建立多人联机通道',                                'realtime', true,  '{"max_connections": 20}'::jsonb,     NULL);

;

;
