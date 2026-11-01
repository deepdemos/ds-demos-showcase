-- 40-entity.sql（抽取自 docs 的 DDL 块；后续以本文件为准）

-- ===== tasks  (docs/实体域数据结构设计.md:241) =====
CREATE TABLE tasks (
    id             bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    slug           text        NOT NULL,
    title          text        NOT NULL,
    title_key      text        NOT NULL DEFAULT '',   -- 去重键（§3.8）
    title_norm     text        NOT NULL DEFAULT '',   -- 软提示键（§3.9）
    brief          text        NOT NULL DEFAULT '',   -- 题目说明（人写的，不是题面）
    category       text,
    status         text        NOT NULL DEFAULT 'active'
                               CHECK (status IN ('active', 'merged', 'hidden')),
    merged_into_id bigint,
    created_by     bigint,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    deleted_at     timestamptz
);

CREATE UNIQUE INDEX uq_tasks_slug ON tasks (slug) WHERE deleted_at IS NULL;

CREATE UNIQUE INDEX uq_tasks_title_key ON tasks (title_key)
    WHERE deleted_at IS NULL AND status = 'active';

CREATE INDEX ix_tasks_title_norm ON tasks (title_norm)
    WHERE deleted_at IS NULL AND status = 'active';

CREATE INDEX ix_tasks_status ON tasks (status) WHERE deleted_at IS NULL;

CREATE INDEX ix_tasks_merged_into ON tasks (merged_into_id) WHERE merged_into_id IS NOT NULL;

-- ===== task_prompts  (docs/实体域数据结构设计.md:294) =====
CREATE TABLE task_prompts (
    task_id    bigint      NOT NULL,
    prompt_id  bigint      NOT NULL,
    role       text        NOT NULL DEFAULT 'variant'
                           CHECK (role IN ('primary', 'variant')),
    note       text        NOT NULL DEFAULT '',   -- 「这个变体改了什么」
    created_by bigint,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (task_id, prompt_id)
);

CREATE INDEX ix_task_prompts_prompt ON task_prompts (prompt_id);

CREATE UNIQUE INDEX uq_task_prompts_primary ON task_prompts (task_id)
    WHERE role = 'primary';

-- ===== prompts  (docs/实体域数据结构设计.md:344) =====
CREATE TABLE prompts (
    id           bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    content_hash text        NOT NULL UNIQUE,          -- sha256(规范化后的 content)
    content      text        NOT NULL,
    created_by   bigint,
    created_at   timestamptz NOT NULL DEFAULT now()
);

-- ===== prompt_library_entries  (docs/实体域数据结构设计.md:370) =====
CREATE TABLE prompt_library_entries (
    id          bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    prompt_id   bigint      NOT NULL,
    scope       text        NOT NULL DEFAULT 'deep'
                            CHECK (scope IN ('deep', 'astra')),
    title       text,                          -- 内容列：擦除置 NULL（v2.7 去掉 NOT NULL）
    category    text,
    source_url  text,
    created_by  bigint,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    deleted_at  timestamptz,
    erased_at   timestamptz,                   -- 擦除时间（v2.7）
    UNIQUE (prompt_id, scope)
);

CREATE INDEX ix_ple_scope ON prompt_library_entries (scope) WHERE deleted_at IS NULL;

-- ===== task_slug_history  (docs/实体域数据结构设计.md:431) =====
CREATE TABLE task_slug_history (
    slug       text        PRIMARY KEY,
    task_id    bigint      NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX ix_task_slug_history_task ON task_slug_history (task_id);

-- ===== proposals  (docs/实体域数据结构设计.md:623) =====
CREATE TABLE proposals (
    id            bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    kind          text        NOT NULL CHECK (kind IN (
                      'new_task', 'new_prompt', 'new_model',
                      'new_classification', 'new_platform', 'new_platform_config', 'new_skill',
                      'new_tag_value',
                      'task_match', 'model_alias', 'merge_task', 'merge_model',
                      'retag_demo', 'report'
                  )),
    -- 取值域与迁移见 标准对象 §ST-6
    status        text        NOT NULL DEFAULT 'pending'
                              CHECK (status IN ('pending', 'approved', 'rejected',
                                                'withdrawn', 'superseded')),
    payload       jsonb,                       -- 内容列：擦除置 NULL（v2.7 去掉 NOT NULL）

    -- 提交
    created_by    bigint,
    created_at    timestamptz NOT NULL DEFAULT now(),

    -- 审核
    reviewed_by   bigint,
    reviewed_at   timestamptz,
    reject_reason text,

    -- 执行结果：让「提案 → 产物」可追溯
    result_kind   text,
    result_id     bigint,

    -- 机器来源
    source        text        NOT NULL DEFAULT 'user'
                              CHECK (source IN ('user', 'admin', 'ai',
                                                'inferred', 'external', 'imported')),
    confidence    real        CHECK (confidence IS NULL
                                     OR (confidence >= 0 AND confidence <= 1)),

    -- 去重：应用层算，DB 只保证 pending 内唯一
    dedupe_key    text,
    updated_at    timestamptz NOT NULL DEFAULT now(),

    -- 生命周期
    erased_at     timestamptz,                 -- 擦除时间（v2.7）

    -- 擦除后 payload 为 NULL：内容没了，`kind` 的必填子集校验自然不适用。
    -- 必须先判 NULL——否则 `payload ? 'title'` 对 NULL 求值是 NULL，整条 CHECK 变 UNKNOWN，
    -- 行反而能过；但别的分支会误判，所以显式写出来。
    CONSTRAINT ck_proposals_payload CHECK (
        payload IS NULL
     OR (kind = 'new_task'      AND payload ? 'title')
     OR (kind = 'new_prompt'    AND payload ? 'title' AND payload ? 'content')
     OR (kind = 'new_model'     AND payload ? 'name')
     OR (kind = 'new_classification'   AND payload ? 'dimension' AND payload ? 'slug')
     OR (kind = 'new_platform'         AND payload ? 'slug' AND payload ? 'name'
                                      AND jsonb_typeof(payload -> 'is_harness') = 'boolean'
                                      AND jsonb_typeof(payload -> 'forms') = 'array'
                                      AND jsonb_array_length(payload -> 'forms') > 0)
     OR (kind = 'new_platform_config'  AND payload ? 'kind' AND payload ? 'slug'
                                      AND payload ? 'name')
     OR (kind = 'new_skill'            AND payload ? 'slug' AND payload ? 'name')
     OR (kind = 'new_tag_value' AND payload ? 'key' AND payload ? 'value')
     OR (kind = 'task_match'    AND payload ? 'task_id' AND payload ? 'demo_id')
     OR (kind = 'model_alias'   AND payload ? 'model_id' AND payload ? 'alias')
     OR (kind IN ('merge_task', 'merge_model')
                                AND payload ? 'source_id' AND payload ? 'target_id')
     OR (kind = 'retag_demo'    AND payload ? 'demo_id' AND payload ? 'add')
     OR (kind = 'report'        AND payload ? 'target_type' AND payload ? 'target_id')
    ),

    CONSTRAINT ck_proposals_review CHECK (
        (status = 'pending' AND reviewed_by IS NULL AND reviewed_at IS NULL)
     OR (status IN ('approved', 'rejected') AND reviewed_at IS NOT NULL)
     OR (status IN ('withdrawn', 'superseded'))
    )
);

CREATE UNIQUE INDEX uq_proposals_pending_dedupe ON proposals (kind, dedupe_key)
    WHERE status = 'pending' AND dedupe_key IS NOT NULL;

CREATE INDEX ix_proposals_inbox  ON proposals (created_at DESC) WHERE status = 'pending';

CREATE INDEX ix_proposals_author ON proposals (created_by, created_at DESC);

CREATE INDEX ix_proposals_kind   ON proposals (kind, status);

-- ===== proposal_refs  (docs/实体域数据结构设计.md:752) =====
CREATE TABLE proposal_refs (
    proposal_id bigint NOT NULL,
    role        text   NOT NULL CHECK (role IN ('subject', 'target', 'source')),
    demo_id     bigint,
    task_id     bigint,
    model_id    bigint,
    PRIMARY KEY (proposal_id, role),
    CONSTRAINT ck_proposal_refs_exactly_one CHECK (
        (demo_id IS NOT NULL)::int + (task_id IS NOT NULL)::int
      + (model_id IS NOT NULL)::int = 1
    )
);

CREATE INDEX ix_proposal_refs_demo  ON proposal_refs (demo_id)  WHERE demo_id  IS NOT NULL;

CREATE INDEX ix_proposal_refs_task  ON proposal_refs (task_id)  WHERE task_id  IS NOT NULL;

CREATE INDEX ix_proposal_refs_model ON proposal_refs (model_id) WHERE model_id IS NOT NULL;

