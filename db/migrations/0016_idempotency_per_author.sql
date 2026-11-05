-- 0016_idempotency_per_author.sql —— 幂等键的唯一范围收到「作者 × 键」
--
-- 基线（`db/schema/30-demo.sql:45`）上是 `UNIQUE (idempotency_key) WHERE NOT NULL`：
-- 全库一把钥匙。接上幂等逻辑之后立刻暴露两个问题：
--
-- 1. **它不幂等，它是抢注**。两个作者各自的客户端都生成了 `"1"` 这种键（脚本作者
--    很可能就用自增序号），第二个人的投稿不是「重放」，是撞车——而系统会告诉他
--    「这条作品早就存在」，还回别人的 slug。
-- 2. **它能探针**。报一个键就知道「有人用过这个键」，等于一个能问「这个字符串
--    被谁用过」的接口，尽管只回答存在与否。
--
-- 幂等键的语义是「**这次调用**的唯一标识」，而调用是有主体的——所以键空间必须
-- 按主体切分。这与 `demo_views.viewer_key`、`demo_ratings.rater_key` 是同一条口径：
-- 判定口径里带上「人」，别指望字符串全局唯一。
--
-- 代价：同一个键在两个作者下各有一行。这不是冲突，是两件事。
-- 查询侧 `FindDemoByIdempotencyKey` 本来就带 `author_id`，这里只是让索引与查询说得上是同一件事。

DROP INDEX IF EXISTS uq_demos_idempotency_key;

CREATE UNIQUE INDEX uq_demos_idempotency_key ON demos (author_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL;

COMMENT ON INDEX uq_demos_idempotency_key IS
    '幂等键在作者范围内唯一：同一个作者的同一个键只落一行，跨作者互不影响。';
