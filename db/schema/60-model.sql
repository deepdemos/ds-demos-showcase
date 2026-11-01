-- 60-model.sql（抽取自 docs 的 DDL 块；后续以本文件为准）

-- ===== demo_revision_models  (docs/模型域设计.md:122) =====
CREATE TABLE demo_revision_models (
    revision_id  bigint NOT NULL,
    model_id     bigint NOT NULL,
    provider_id  bigint,
    serving_note text NOT NULL DEFAULT '',        -- 沿用 v1.1：fp8 / free tier / context 32k（≤100 字符）
    source       text NOT NULL DEFAULT 'api',     -- 枚举：api | legacy_tag | migration | attribution | merge
    created_at   timestamptz NOT NULL DEFAULT now(), -- 沿用 v1.1
    PRIMARY KEY (revision_id, model_id)           -- 同一模型经不同 provider 算一条
);

CREATE INDEX ix_demo_revision_models_model    ON demo_revision_models (model_id);

CREATE INDEX ix_demo_revision_models_provider ON demo_revision_models (provider_id);

-- ===== vendors  (docs/模型域设计.md:765) =====
CREATE TABLE vendors (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    slug         text NOT NULL UNIQUE,
    name         text NOT NULL,
    homepage_url text NOT NULL DEFAULT '',
    status       text NOT NULL DEFAULT 'active',
    source       text NOT NULL DEFAULT 'manual',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- ===== providers  (docs/模型域设计.md:765) =====
CREATE TABLE providers (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    slug         text NOT NULL UNIQUE,
    name         text NOT NULL,
    vendor_id    bigint,
    kind         text NOT NULL DEFAULT 'aggregator',
    homepage_url text NOT NULL DEFAULT '',
    description  text NOT NULL DEFAULT '',
    status       text NOT NULL DEFAULT 'active',
    source       text NOT NULL DEFAULT 'manual',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX ix_providers_vendor ON providers (vendor_id);

-- ===== model_specs  (docs/模型域设计.md:765) =====
CREATE TABLE model_specs (
    model_id            bigint PRIMARY KEY,
    modalities_in       text NOT NULL DEFAULT '',
    modalities_out      text NOT NULL DEFAULT '',
    context_window      integer,
    max_output          integer,
    param_total_b       double precision,
    param_active_b      double precision,
    param_kind          text NOT NULL DEFAULT 'undisclosed',
    param_note          text NOT NULL DEFAULT '',
    release_date        date,
    knowledge_cutoff    text,
    open_weights        integer,
    cost_in             double precision,
    cost_out            double precision,
    license             text,
    canonical_model_id  text,
    source              text NOT NULL DEFAULT 'manual',
    fetched_at          timestamptz,
    note                text NOT NULL DEFAULT ''
);

-- ===== model_catalog_items  (docs/模型域设计.md:765) =====
-- ---------- models.dev 导入与同步 ----------

CREATE TABLE model_catalog_items (
    id                         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    source                     text NOT NULL DEFAULT 'models.dev',
    provider_id                text NOT NULL,
    provider_name              text NOT NULL,
    external_id                text NOT NULL,
    name                       text NOT NULL,
    canonical_id               text,
    vendor_slug                text,
    family                     text,
    description                text NOT NULL DEFAULT '',
    modalities_in              text NOT NULL DEFAULT '',
    modalities_out             text NOT NULL DEFAULT '',
    context_window             integer,
    max_output                 integer,
    cost_in                    double precision,
    cost_out                   double precision,
    release_date               date,
    knowledge_cutoff           text,
    open_weights               integer,
    model_type                 text,
    upstream_status            text,
    supports_tool_call         integer,
    supports_reasoning         integer,
    supports_structured_output integer,
    supports_attachment        integer,
    raw_json                   text NOT NULL DEFAULT '',
    fetched_at                 timestamptz NOT NULL DEFAULT now(),
    UNIQUE (source, provider_id, external_id)
);

CREATE INDEX ix_catalog_canonical ON model_catalog_items (source, canonical_id);

CREATE INDEX ix_catalog_provider  ON model_catalog_items (source, provider_id);

CREATE INDEX ix_catalog_vendor    ON model_catalog_items (source, vendor_slug);

-- ===== model_external_refs  (docs/模型域设计.md:765) =====
CREATE TABLE model_external_refs (
    id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    model_id      bigint NOT NULL,
    source        text NOT NULL DEFAULT 'models.dev',
    provider_id   text NOT NULL,
    provider_name text NOT NULL DEFAULT '',
    external_id   text NOT NULL,
    canonical_id  text,
    is_official   boolean NOT NULL DEFAULT false,
    created_at    timestamptz NOT NULL DEFAULT now(),
    UNIQUE (source, provider_id, external_id)
);

CREATE INDEX ix_external_refs_model     ON model_external_refs (model_id);

CREATE INDEX ix_external_refs_canonical ON model_external_refs (source, canonical_id);

-- ===== model_spec_fields  (docs/模型域设计.md:765) =====
CREATE TABLE model_spec_fields (
    model_id   bigint NOT NULL,
    field      text NOT NULL,
    source     text NOT NULL DEFAULT 'manual',
    locked     boolean NOT NULL DEFAULT false,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (model_id, field)
);

-- ===== model_source_sync  (docs/模型域设计.md:765) =====
CREATE TABLE model_source_sync (
    source          text PRIMARY KEY,
    last_attempt_at timestamptz,
    last_success_at timestamptz,
    payload_hash    text,
    item_count      bigint,
    status          text NOT NULL DEFAULT 'stale',
    error           text NOT NULL DEFAULT ''
);

-- ===== models（现有表，结构沿用旧栈；模型域 §3.2「结构不动，语义收紧」）=====
CREATE TABLE models (
    id             bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    slug           text        NOT NULL UNIQUE,
    name           text        NOT NULL UNIQUE,
    vendor         text,
    vendor_id      bigint,
    status         text        NOT NULL DEFAULT 'candidate',
    resolution     text        NOT NULL DEFAULT 'exact',
    merged_into_id bigint,
    description    text        NOT NULL DEFAULT '',
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT ck_models_slug_len CHECK (length(slug) BETWEEN 1 AND 128),
    CONSTRAINT ck_models_name_len CHECK (length(name) BETWEEN 1 AND 128),
    CONSTRAINT ck_models_vendor_len CHECK (vendor IS NULL OR length(vendor) <= 64),
    CONSTRAINT ck_models_status    CHECK (status IN ('candidate', 'active', 'unverified', 'deprecated')),
    CONSTRAINT ck_models_resolution CHECK (resolution IN ('exact', 'family', 'unknown', 'guess')),
    CONSTRAINT ck_models_desc_len  CHECK (length(description) <= 1000),
    CONSTRAINT ck_models_no_self   CHECK (merged_into_id IS NULL OR merged_into_id <> id)
);

CREATE INDEX ix_models_status     ON models (status);

CREATE INDEX ix_models_resolution ON models (resolution);

CREATE INDEX ix_models_vendor_id  ON models (vendor_id) WHERE vendor_id IS NOT NULL;

-- ===== model_aliases（唯一解析表；规范化写法也存一条）=====
CREATE TABLE model_aliases (
    alias    text   PRIMARY KEY,
    model_id bigint NOT NULL,

    CONSTRAINT ck_alias_len CHECK (length(alias) BETWEEN 1 AND 128)
);

CREATE INDEX ix_model_aliases_model ON model_aliases (model_id);

-- ===== benchmarks  (docs/模型域设计.md:765) =====
CREATE TABLE benchmarks (
    id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    slug          text NOT NULL UNIQUE,
    name          text NOT NULL,
    publisher     text NOT NULL DEFAULT '',
    homepage_url  text NOT NULL DEFAULT '',
    category      text NOT NULL DEFAULT 'general',  -- 多值：逗号枚举固定顺序（如 'coding,math'）；空串按 'general' 处理
    description   text NOT NULL DEFAULT '',
    status        text NOT NULL DEFAULT 'active',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- ===== benchmark_versions  (docs/模型域设计.md:765) =====
CREATE TABLE benchmark_versions (
    id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    benchmark_id      bigint NOT NULL,
    version           text NOT NULL,
    as_of             date NOT NULL,
    source_type       text NOT NULL DEFAULT 'leaderboard',
    coverage          text NOT NULL DEFAULT 'full',
    participant_count bigint,
    score_min         double precision,
    score_max         double precision,
    status            text NOT NULL DEFAULT 'active',
    source_url        text NOT NULL DEFAULT '',
    retrieved_at timestamptz NOT NULL DEFAULT now(),
    note              text NOT NULL DEFAULT '',
    UNIQUE (benchmark_id, version)
);

CREATE INDEX ix_benchmark_versions_asof ON benchmark_versions (benchmark_id, as_of DESC);

-- ===== benchmark_metrics  (docs/模型域设计.md:765) =====
CREATE TABLE benchmark_metrics (
    id               bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    version_id       bigint NOT NULL,
    key              text NOT NULL,
    label            text NOT NULL,
    metric_kind      text NOT NULL DEFAULT 'score',
    unit             text NOT NULL DEFAULT '',
    scale_min        double precision,
    scale_max        double precision,
    higher_is_better boolean NOT NULL DEFAULT true,
    is_primary       boolean NOT NULL DEFAULT false,
    sort             integer NOT NULL DEFAULT 0,
    note             text NOT NULL DEFAULT '',
    UNIQUE (version_id, key)
);

CREATE UNIQUE INDEX uq_benchmark_metrics_primary
    ON benchmark_metrics (version_id) WHERE is_primary;

-- ===== benchmark_scores  (docs/模型域设计.md:765) =====
CREATE TABLE benchmark_scores (
    id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    metric_id         bigint NOT NULL,
    model_id          bigint,
    external_name     text NOT NULL,
    external_name_key text NOT NULL,
    match_status      text NOT NULL DEFAULT 'unmatched',
    raw_score         double precision NOT NULL,
    rank              integer,
    ranked            boolean NOT NULL DEFAULT true,
    source_url        text NOT NULL DEFAULT '',
    retrieved_at      timestamptz NOT NULL DEFAULT now(),
    created_at        timestamptz NOT NULL DEFAULT now(),
    note              text NOT NULL DEFAULT '',
    UNIQUE (metric_id, external_name_key)
);

CREATE INDEX ix_benchmark_scores_model ON benchmark_scores (model_id, metric_id);

CREATE INDEX ix_benchmark_scores_rank  ON benchmark_scores (metric_id, rank);

-- ===== model_tag_archive  (docs/模型域设计.md:1160) =====
CREATE TABLE model_tag_archive (
    id              bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    original_tag_id bigint      NOT NULL,   -- 归档前的 tags.id（不设 FK：tags 行随后删除）
    key             text        NOT NULL,   -- 恒为 'model'
    value           text        NOT NULL,   -- 归档时的 tags.value（= 当时的 Model.name）
    tag_group       text,                   -- tags.group（厂商快照）
    tag_status      text        NOT NULL,   -- active / deprecated
    description     text        NOT NULL DEFAULT '',
    demo_ids        jsonb       NOT NULL DEFAULT '[]'::jsonb,  -- 该标签当时挂的作品（demo_tags 快照）
    demo_count      integer     NOT NULL DEFAULT 0,            -- 冗余，便于与 jsonb 长度核对
    archived_at     timestamptz NOT NULL DEFAULT now(),
    archive_reason  text        NOT NULL DEFAULT 'model 标签退场（T6）',
    restored_at     timestamptz,            -- A6 恢复后标记；非空 = 已写回
    UNIQUE (original_tag_id)
);

CREATE INDEX ix_model_tag_archive_pending ON model_tag_archive (archived_at)
    WHERE restored_at IS NULL;
