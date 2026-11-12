-- 0029：给模型加「版本」（checkpoint），并让作品与调用行都能指到某一版。
--
-- 为什么需要这张表：同一个模型名底下其实叠着好几版快照（`deepseek-v4.1-flash` 与
-- `deepseek-v4.1-flash-1017` 是同一个模型的两版）。现在作品只记 `model_id`，「我用的是
-- 十月中旬那版」这句话无处安放——写进备注就是散文，写进 model_id 就得再建一个模型，
-- 后者会把「同一个模型的版本」和「两个不同模型」压成同一层，合并、去重、对比全线受影响。
-- 版本是模型的子实体，所以单开一张从属表。
--
-- 键为什么是 (model_id, label) 而不是全局唯一 label：版本号只在模型内部有意义，
-- 「1017」在 A 模型下指 A 的那一版、在 B 模型下指 B 的那一版，硬拉成全局唯一就得给 label
-- 拼前缀，等于把复合键写进字符串。
--
-- `released_on` 可空：上游给的日期不一定有（有的只写「2026-10」），空着比编一个准确；
-- 不设「不能晚于今天」之类的约束——版本预发布、日期写错都要允许人工修正，日期本身不是判据。
--
-- 两条引用列都 ON DELETE RESTRICT：一个 checkpoint 一旦被作品或调用行引用，它就是
-- 「当时用的是哪一版」这句话的一部分，删掉它等于改写历史。要删就先解除引用。
-- `demo_revision_models.checkpoint_id` 与 `model_offerings.checkpoint_id` 都允许 NULL：
-- 存量行、以及只知道模型不知道版本的投稿与上报，NULL 就是「未指定」，不是「默认最新版」。
-- 这层语义刻意不用哨兵行（NULL 与 `'unknown'` 两种写法会让每个读的地方都要判一遍）。
--
-- `model_offerings.pinning` 与 checkpoint 分开：checkpoint 说的是「这是哪一版」，
-- pinning 说的是「这个 external_id 是否钉死到某一版」——端点上报的 id 常常是滚动的别名
-- （`deepseek-chat` 指向当前版），硬当一个版本理解就会把历史串味。取值三种：
-- exact（钉死）/ rolling（滚动）/ unknown（不知道，默认）。默认 unknown 保证存量行
-- 与还没判定的写入方都不会假装知道；这一列在 Step 2 只管存，判据等端点写入方落地再定。
--
-- 不改 `db/schema/`（冻结基线，等于 0000 号迁移），也不动 `models.merged_into_id`
-- 那条自引用（它本来就没有外键，合并链靠递归查询走）。
--
-- 存量数据零影响：新表是空的，三个新列都可空/defaulted，不写任何 UPDATE。

CREATE TABLE model_checkpoints (
    id          bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    model_id    bigint      NOT NULL,
    label       text        NOT NULL,
    released_on date,
    notes       text        NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT ck_model_checkpoints_label CHECK (length(label) BETWEEN 1 AND 64),
    UNIQUE (model_id, label)
);

CREATE INDEX ix_model_checkpoints_model ON model_checkpoints (model_id);

ALTER TABLE model_checkpoints ADD CONSTRAINT fk_model_checkpoints_model_id70
    FOREIGN KEY (model_id) REFERENCES models (id) ON DELETE RESTRICT;

ALTER TABLE demo_revision_models ADD COLUMN checkpoint_id bigint;

ALTER TABLE demo_revision_models ADD CONSTRAINT fk_demo_revision_models_checkpoint_id71
    FOREIGN KEY (checkpoint_id) REFERENCES model_checkpoints (id) ON DELETE RESTRICT;

ALTER TABLE model_offerings ADD COLUMN checkpoint_id bigint;

ALTER TABLE model_offerings ADD CONSTRAINT fk_model_offerings_checkpoint_id72
    FOREIGN KEY (checkpoint_id) REFERENCES model_checkpoints (id) ON DELETE RESTRICT;

ALTER TABLE model_offerings ADD COLUMN pinning text NOT NULL DEFAULT 'unknown';

ALTER TABLE model_offerings ADD CONSTRAINT ck_model_offerings_pinning
    CHECK (pinning IN ('exact', 'rolling', 'unknown'));
