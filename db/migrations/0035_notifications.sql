-- 0035：通知域（#437，NTF-001 – NTF-008）。
--
-- notification_kinds 是事件类型注册表：加一类通知 = 插一行，不改 notifications 结构
-- （NTF-003）。notifications 只存对象快照（slug + 当时标题），不存 demo 外键——
-- 外键 CASCADE 会让「作品删了通知也没了」，违反 NTF-007。
--
-- 未读 = read_at IS NULL。保留期读时清理，站点参数 notification.retention_days。

CREATE TABLE notification_kinds (
    key         text        PRIMARY KEY,
    label       text        NOT NULL,
    object_type text        NOT NULL DEFAULT 'demo',
    created_at  timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT ck_notification_kinds_key CHECK (key ~ '^[a-z][a-z0-9._-]{1,63}$')
);

INSERT INTO notification_kinds (key, label) VALUES
    ('review.approved', '作品审核通过'),
    ('review.rejected', '作品被驳回'),
    ('rating.received', '作品收到评分')
ON CONFLICT (key) DO NOTHING;

CREATE TABLE notifications (
    id         bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    account_id bigint      NOT NULL,
    kind       text        NOT NULL,
    demo_slug  text        NOT NULL,
    demo_title text        NOT NULL DEFAULT '',
    payload    jsonb       NOT NULL DEFAULT '{}'::jsonb,
    read_at    timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT fk_notifications_account
        FOREIGN KEY (account_id) REFERENCES accounts (id) ON DELETE CASCADE,
    CONSTRAINT fk_notifications_kind
        FOREIGN KEY (kind) REFERENCES notification_kinds (key),
    CONSTRAINT ck_notifications_slug
        CHECK (char_length(demo_slug) BETWEEN 1 AND 128),
    CONSTRAINT ck_notifications_title
        CHECK (char_length(demo_title) <= 200)
);

-- 列表：按时间新→旧，id 兜同秒排序。
CREATE INDEX ix_notifications_time
    ON notifications (account_id, created_at DESC, id DESC);

-- 未读计数：部分索引只覆盖未读行。
CREATE INDEX ix_notifications_unread
    ON notifications (account_id) WHERE read_at IS NULL;

COMMENT ON TABLE notification_kinds IS
    '通知事件类型注册表（NTF-003）：加类型不改变 notifications 表结构。';
COMMENT ON TABLE notifications IS
    '站内通知：对象用 slug + 标题快照，不存外键，作品删除后仍可读（NTF-007）。';
