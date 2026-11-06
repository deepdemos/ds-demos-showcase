-- 「上不上列表」的唯一定义处。
--
-- 已有的 `demo_visible_channels`（0017）回答的是另一个问题：「这件作品对外可见吗」
-- （listed 与 unlisted 都算——unlisted 可直链、只是不上列表）。两个问题不同，
-- 所以需要两张视图，而不是把 visibility 判据散在各处再靠人工保持一致。
--
-- 为什么现在补：分类目录上的「游戏 · 230 件」这个数字必须与点进去的列表条数一致。
-- 而那条计数写在 `classifications.sql` 里，如果它自己再抄一遍
-- `visibility = 'listed'`，筛选器与列表就成了两个事实源——一边多算 hidden、
-- 一边少算 published_revision，用户看到的是「230 件」点进去只有 12 条。
--
-- `check-single-source` 抓到过这一处（门禁数的是文本，所以它抓得挺准）。

CREATE VIEW demo_listed_channels AS
SELECT demo_id, channel, visibility, lang, published_at, created_at, updated_at
  FROM demo_channels
 WHERE visibility = 'listed';

COMMENT ON VIEW demo_listed_channels IS
  '上列表的渠道行（visibility = listed）。「对外可见」用 demo_visible_channels（0017），两者不等价：unlisted 可直链但不上列表。';
