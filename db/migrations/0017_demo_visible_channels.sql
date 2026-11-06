-- 0017: 把「对外可见的渠道」收成一个视图。
--
-- # 为什么要有这个视图
--
-- 「这个作品对外可见吗」在 2026-11-04 那次 review 里同根了 6 条：SQL 里 5 处
-- `visibility IN ('listed','unlisted')`、Go 里若干处、前端 1 处，各自漏一点。
-- 单一出处门禁把 SQL 侧压到 demos.sql 一个文件里的 5 处，但**同一个文件里仍有
-- 5 份**——只要有一处忘了写，那个入口就又开一次洞：发布时间子查询就是这么漏的
-- （它取的是全部渠道的最早时间，于是「先发 hidden 内部预览」的作品，详情页上
-- 显示的发布时间会比真正对外的那天早几个月）。
--
-- 视图让判据只有一个定义：调用处只问「这个 demo 在不在 demo_visible_channels 里」，
-- 不再各写一遍可见性枚举。
--
-- # 为什么不是函数
--
-- 函数要在每条查询里调用，pg 不会帮你内联成集合；视图可以直接 EXISTS / join，
-- 也能被索引条件下推。语义上这也确实是「一张表的一个子集」，不是一次计算。
--
-- # 与 demo_channels 的关系
--
-- 纯查询层：不搬数据、不加触发器、不改 demo_channels 的任何约束。
-- 回退 = DROP VIEW 并把调用处改回来。
CREATE VIEW demo_visible_channels AS
SELECT c.id, c.demo_id, c.channel, c.visibility, c.lang,
       c.featured, c.featured_order, c.published_at, c.curated_by,
       c.created_at, c.updated_at
  FROM demo_channels c
 WHERE c.visibility IN ('listed', 'unlisted');

COMMENT ON VIEW demo_visible_channels IS
    '对外可见的渠道行（listed 与 unlisted）。可见性判据的唯一定义处，调用方不要自己写枚举。';
