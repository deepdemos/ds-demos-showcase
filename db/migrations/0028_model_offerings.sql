-- 0028：`model_external_refs` 改成 `model_offerings`（一张调用表两种出处）
--
-- 「这张模型是怎么被指到的」原先只有一处出处：models.dev 的目录条目，
-- 存在 `model_external_refs (source, provider_id, external_id) → model_id`。
-- 将来端点上报（插件说「我调的是 api.deepseek.com/v1 上的 deepseek-v4-flash-1017」）
-- 要落在同一张表里，区别只在「谁说的」——所以把 `source` 改名 `authority`，
-- 表名改成它真正记的东西：一次**调用**（authority + external_id → model）。
--
-- 键为什么必须带上 provider_id：真实目录数据的实测（8988 条目录条目）—
-- distinct (provider_id, external_id) = 8988，distinct external_id 只有 4131，
-- **1182 个 external_id 跨多个 provider 复用**。只用 (authority, external_id)
-- 会在迁移这一步就撞唯一约束。设计稿 #404 写的「(authority, external_id)」
-- 是简写，落地必须带 provider 维度；端点类条目的 provider_id 留空串。
--
-- 老表的三条附属物（fk_model_external_refs_model_id69 / ix_external_refs_model /
-- ix_external_refs_canonical）随 DROP TABLE 一起消失，新的在下面对应建回。
--
-- 新表比老表多了两条长度 CHECK（authority 1..128 / external_id 1..256）：老表没有，
-- 这是刻意收紧——两列都会被端点上报写入，长度不设上限等于让外部输入决定索引宽度。
-- 迁移当天库里的最长值是 authority 10 / external_id 65（目录镜像的 provider_id 另有最长
-- 24，本表没给它设 CHECK），搬行不会失败。
--
-- 这两条**没有**登记到 `docs/运行手册-可变常量.md`：它们不是运营可调的口径，而是外部输入
-- 的入口宽度，值由端点上报的实测分布决定。等端点写入方真正落地（#445 的后续刀）时，
-- 连同「超长上报怎么回 400」一起登记，免得现在登记的值变成没人维护的样子货。

CREATE TABLE model_offerings (
    id            bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    authority     text        NOT NULL,
    provider_id   text        NOT NULL DEFAULT '',
    provider_name text        NOT NULL DEFAULT '',
    external_id   text        NOT NULL,
    model_id      bigint      NOT NULL,
    canonical_id  text,
    is_official   boolean     NOT NULL DEFAULT false,
    created_at    timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT ck_model_offerings_authority CHECK (length(authority) BETWEEN 1 AND 128),
    CONSTRAINT ck_model_offerings_external_id CHECK (length(external_id) BETWEEN 1 AND 256),
    UNIQUE (authority, provider_id, external_id)
);

-- 存量原样搬过来：这条迁移只改名、只加 authority 的语义，不改任何一行的归属。
-- 老表的 (source, provider_id, external_id) 与新表的唯一键一一对应，
-- id 不保留（没有外部引用它，与 catalog 那边同一条理由）。
INSERT INTO model_offerings (authority, provider_id, provider_name, external_id, model_id, canonical_id, is_official, created_at)
SELECT source, provider_id, provider_name, external_id, model_id, canonical_id, is_official, created_at
FROM model_external_refs;

CREATE INDEX ix_model_offerings_model ON model_offerings (model_id);

-- 与老表的 ix_external_refs_canonical 对应：`GetModelDiffTarget` 的 canonical 分支按
-- (source, canonical_id) 找行，没有这条索引就会退化成全表扫描。
CREATE INDEX ix_model_offerings_canonical ON model_offerings (authority, canonical_id);

ALTER TABLE model_offerings ADD CONSTRAINT fk_model_offerings_model_id69
    FOREIGN KEY (model_id) REFERENCES models (id) ON DELETE CASCADE;

DROP TABLE model_external_refs;
