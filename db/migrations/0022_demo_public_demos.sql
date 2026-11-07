-- 公开可见的作品：作品库、模型页、分类计数都该问同一张视图。
--
-- 为什么现在补：模型页要算「某模型有多少件公开作品」，如果再手写一遍
-- `moderation_state='approved' AND lifecycle_state='published' AND deleted_at IS NULL
-- AND EXISTS (listed 渠道)`，就是第 N 份副本。分类目录已经因此翻过一次车
--（0019 的注释里记着：一边显示 230 件、点进去只有 12 条）。
--
-- 与两张已有视图的分工：
--   demo_visible_channels（0017）——「这件作品对外可见吗」，listed 与 unlisted 都算
--   demo_listed_channels（0019）——「这件作品上不上列表」，只算 listed
--   demo_public_demos（本文件）——「这件作品算不算公开作品」，状态三判据 + 上列表
--
-- 本视图只出 id：调用方要什么自己去 join demos，避免视图变成一张「什么都往上加」的宽表。
--
-- 存量债：`db/queries/demos.sql` 与 `db/queries/classifications.sql` 里仍各有一份手写判据
--（单一出处门禁的基线里记着）。新代码一律用本视图；那两处等下次改到再收敛。
CREATE VIEW demo_public_demos AS
SELECT d.id AS demo_id
  FROM demos d
 WHERE d.moderation_state = 'approved'
   AND d.lifecycle_state = 'published'
   AND d.deleted_at IS NULL
   AND d.erased_at IS NULL
   AND EXISTS (
       SELECT 1 FROM demo_listed_channels c WHERE c.demo_id = d.id
   );

COMMENT ON VIEW demo_public_demos IS
    '公开可见作品的 id 集合（approved + published + 未删 + 有 listed 渠道）。公开性的唯一定义处。';
