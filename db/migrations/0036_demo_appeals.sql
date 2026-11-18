-- 0036：作品申诉（#463）。
--
-- 一表一语义：申诉不是提案（治理域 D4）。提案是「建议新增/修改」，申诉是
-- 「对一次已发生的处置提出异议」；通过后的动作也不同（提案写新业务表，申诉恢复既有作品）。
--
-- 同一作品同时最多一条 pending（部分唯一索引）；被驳回后允许再提。

CREATE TABLE demo_appeals (
    id                   bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    demo_id              bigint      NOT NULL,
    appellant_account_id bigint      NOT NULL,
    reason               text        NOT NULL,
    status               text        NOT NULL DEFAULT 'pending'
                                     CHECK (status IN ('pending', 'approved', 'rejected')),
    reviewed_by          bigint,
    reviewed_at          timestamptz,
    decision_reason      text        NOT NULL DEFAULT '',
    created_at           timestamptz NOT NULL DEFAULT now(),
    updated_at           timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT fk_demo_appeals_demo
        FOREIGN KEY (demo_id) REFERENCES demos (id) ON DELETE CASCADE,
    CONSTRAINT fk_demo_appeals_appellant
        FOREIGN KEY (appellant_account_id) REFERENCES accounts (id) ON DELETE CASCADE,
    CONSTRAINT fk_demo_appeals_reviewer
        FOREIGN KEY (reviewed_by) REFERENCES users (id) ON DELETE SET NULL,
    CONSTRAINT ck_demo_appeals_reason
        CHECK (char_length(reason) BETWEEN 1 AND 2000),
    CONSTRAINT ck_demo_appeals_decision_reason
        CHECK (char_length(decision_reason) <= 500),
    -- pending 不能有裁决字段；已裁决必须带 reviewed_at。
    CONSTRAINT ck_demo_appeals_review CHECK (
        (status = 'pending' AND reviewed_at IS NULL)
     OR (status IN ('approved', 'rejected') AND reviewed_at IS NOT NULL)
    )
);

CREATE UNIQUE INDEX uq_demo_appeals_pending
    ON demo_appeals (demo_id) WHERE status = 'pending';

CREATE INDEX ix_demo_appeals_demo_time
    ON demo_appeals (demo_id, created_at DESC);

CREATE INDEX ix_demo_appeals_status_time
    ON demo_appeals (status, created_at) WHERE status = 'pending';

COMMENT ON TABLE demo_appeals IS
    '作品申诉（#463）：一表一语义；同作品同时最多一条 pending；通过后恢复 removed 作品。';
