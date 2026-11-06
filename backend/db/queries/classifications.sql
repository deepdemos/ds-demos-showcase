-- 分类目录、作品分类的读写。
--
-- 数据从哪来：`db/migrations/0018_classification_seed.sql`（旧站三个内容分类键
-- 归并成 92 条）。本文件只负责读与写，不负责词表内容。
--
-- # 维度顺序的唯一出处
--
-- 三个维度的顺序（形态 → 技术 → 题材）记在 `classification_dimensions.sort` 里，
-- 所以这里的每条查询都 join 它拿顺序，而不是写 `CASE WHEN dimension='genre' THEN 0`。
-- 按字母排会得到 genre → subject → technique，那是错的。

-- name: ListDimensions :many
-- 维度元数据。列表页要按它分组显示，所以带 label / sort / mode。
-- 只出 active：deprecated 的维度留着行是为了让老数据还能 join 出名字。
SELECT dimension, label, description, mode, tier, sort
FROM classification_dimensions
WHERE status = 'active'
ORDER BY sort, dimension;

-- name: ListClassifications :many
-- 全部可用的分类项，带**已发布作品**的引用数。
--
-- 计数口径必须与 ListPublishedDemos 的可见性判据一致（见 demos.sql 文件头），
-- 否则筛选器上写着「游戏 230 件」而点进去列表只有 12 条——同一件事的两个答案。
-- 用 EXISTS 而不是 join：一件作品在同维度挂两项时不该被数两次。
SELECT c.id, c.dimension, c.slug, c.label, c.description, c.parent_id, c.aliases, c.sort,
       dm.sort AS dimension_sort,
       (
           SELECT count(*)
           FROM demo_classifications dc
           JOIN demos d ON d.id = dc.demo_id
           WHERE dc.classification_id = c.id
             AND d.published_revision_id IS NOT NULL
             AND d.lifecycle_state = 'published'
             AND d.deleted_at IS NULL
             AND d.erased_at IS NULL
             AND EXISTS (SELECT 1 FROM demo_listed_channels ch
                         WHERE ch.demo_id = d.id)
       )::int AS demo_count
FROM classifications c
JOIN classification_dimensions dm ON dm.dimension = c.dimension
WHERE c.status = 'active'
ORDER BY dm.sort, c.sort, c.slug;

-- name: GetClassificationsBySlugs :many
-- 投稿与更新时把一批 slug 解析成 id。
--
-- **不按 status 过滤**：已 deprecated 的项仍可被历史版本继续引用（删一个分类
-- 不该让老作品报错）。新建引用时调用方只该传 active 的，那是调用方的责任，
-- 这里负责「认得出」。
SELECT id, dimension, slug, status
FROM classifications
WHERE slug = ANY(sqlc.arg(slugs)::text[]);

-- name: InsertDemoClassification :exec
INSERT INTO demo_classifications (demo_id, dimension, classification_id)
VALUES ($1, $2, $3)
ON CONFLICT (demo_id, dimension, classification_id) DO NOTHING;

-- name: DeleteDemoClassifications :exec
-- 整体替换语义：先清空这件作品的全部分类，再按新的一批插入。
-- 与 modelSlugs 一致（multipart 里「没传」与「传空」无法区分）。
DELETE FROM demo_classifications WHERE demo_id = $1;

-- name: ListDemoClassifications :many
SELECT c.dimension, c.slug, c.label
FROM demo_classifications dc
JOIN classifications c ON c.id = dc.classification_id
JOIN classification_dimensions dm ON dm.dimension = c.dimension
WHERE dc.demo_id = $1
ORDER BY dm.sort, c.sort, c.slug;

-- name: ListClassificationsForDemos :many
-- 列表页一次取回一整页作品的分类（一条查询，不是一件一次）。
--
-- 返回的是扁平行，Go 侧按 demo_id 分组。用 json_agg 一次拼成 JSON 也能做，
-- 但那会把「分类的形状」写进 SQL，而形状是契约的事。
SELECT dc.demo_id, c.dimension, c.slug, c.label
FROM demo_classifications dc
JOIN classifications c ON c.id = dc.classification_id
JOIN classification_dimensions dm ON dm.dimension = c.dimension
WHERE dc.demo_id = ANY(sqlc.arg(demo_ids)::bigint[])
ORDER BY dc.demo_id, dm.sort, c.sort, c.slug;
