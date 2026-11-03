-- 0010：渠道建成主数据表，`demo_channels.channel` 从自由文本改成外键。
--
-- 背景：`docs/标准对象/20-枚举值域.md` §EN-1 明写「渠道自身（deep / astra / 未来渠道）
-- 是数据不是枚举，在 demo_channels.channel 里自由增长」。但「自由增长」被实现成了
-- 「随便写」—— 打错一个字（`astr`）就是一个新渠道，而且不会有任何报错。
--
-- 与 `demo_types` 同一个模式：加一种渠道是加一行数据，不是加一条 migration。

CREATE TABLE channels (
    code       text        PRIMARY KEY,
    name       text        NOT NULL,
    sort       int         NOT NULL DEFAULT 0,
    status     text        NOT NULL DEFAULT 'active'
                           CHECK (status IN ('active', 'deprecated')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT ck_channels_code CHECK (code ~ '^[a-z][a-z0-9_]*$')
);

INSERT INTO channels (code, name, sort) VALUES
    ('deep',  '主站',       10),
    ('astra', 'Astra 橱窗', 20);

-- 加外键前先查孤儿。存量里如果已有拼错的渠道名（比如 'astr'），
-- ADD CONSTRAINT 会以 `violates foreign key constraint` 失败，
-- 报错不告诉你有几行、是哪些值。先算一遍，带清单中止。
DO $$
DECLARE bad text;
BEGIN
    SELECT string_agg(DISTINCT channel, ', ') INTO bad
        FROM demo_channels c
        WHERE NOT EXISTS (SELECT 1 FROM channels x WHERE x.code = c.channel);
    IF bad IS NOT NULL THEN
        RAISE EXCEPTION 'demo_channels.channel 有未登记的取值（%），先清理或先补 channels 行', bad;
    END IF;
END $$;

ALTER TABLE demo_channels ADD CONSTRAINT fk_demo_channels_channel94
    FOREIGN KEY (channel) REFERENCES channels (code);

CREATE INDEX ix_channels_active ON channels (sort) WHERE status = 'active';
