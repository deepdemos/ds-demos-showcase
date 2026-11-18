-- 0034：收藏域（#438，COL-001 – COL-014）。
--
-- 两张表：collections 存夹，collection_items 存夹内作品。
-- 默认夹由应用层惰性创建（不写触发器）：它只是一行 is_default=true 的普通数据，
-- 「每人最多一个默认夹」由部分唯一索引保证。
--
-- 上限（自定义夹 20 / 单夹 500）不在这里写死：读 setting_keys / site_settings
-- 的 collection.max_per_account / collection.max_items（设置域 §4）。
--
-- 墓碑：作品软删时 demos 行还在，收藏条目继续 join 得到标题；
-- 物理删除（GC）时条目随外键 CASCADE 消失——内容不存在了，留空壳没有意义。

CREATE TABLE collections (
    id          bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    account_id  bigint      NOT NULL,
    name        text        NOT NULL,
    description text        NOT NULL DEFAULT '',
    is_default  boolean     NOT NULL DEFAULT false,
    visibility  text        NOT NULL DEFAULT 'private'
                            CHECK (visibility IN ('private', 'public')),
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT fk_collections_account
        FOREIGN KEY (account_id) REFERENCES accounts (id) ON DELETE CASCADE,
    CONSTRAINT ck_collections_name
        CHECK (char_length(name) BETWEEN 1 AND 50 AND name = btrim(name)),
    CONSTRAINT ck_collections_description
        CHECK (char_length(description) <= 500),
    -- 默认夹永远私有（COL-002）：转公开的动作在 handler 里 422，这里再兜一层。
    CONSTRAINT ck_collections_default_private
        CHECK (NOT is_default OR visibility = 'private')
);

-- 每人最多一个默认夹。
CREATE UNIQUE INDEX uq_collections_default
    ON collections (account_id) WHERE is_default;

-- 同一用户下名称大小写不敏感唯一（COL-004）。
CREATE UNIQUE INDEX uq_collections_name
    ON collections (account_id, lower(name));

CREATE TABLE collection_items (
    collection_id bigint      NOT NULL,
    demo_id       bigint      NOT NULL,
    added_at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (collection_id, demo_id),

    CONSTRAINT fk_collection_items_collection
        FOREIGN KEY (collection_id) REFERENCES collections (id) ON DELETE CASCADE,
    CONSTRAINT fk_collection_items_demo
        FOREIGN KEY (demo_id) REFERENCES demos (id) ON DELETE CASCADE
);

-- 夹内按加入时间倒序：列表页的读法。
CREATE INDEX ix_collection_items_time
    ON collection_items (collection_id, added_at DESC);

COMMENT ON TABLE collections IS
    '收藏夹（COL-001–014）：默认夹每人一个、永远私有、不可改名/删除/转公开。';
COMMENT ON TABLE collection_items IS
    '夹内作品：同夹不重复；作品软删时保留条目（墓碑），物理删除时 CASCADE。';
