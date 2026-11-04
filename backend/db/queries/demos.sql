-- 作品列表与详情。
--
-- 「公开可见」的判定口径（列表、详情、预览三处必须一致，改一处就要改其余两处）：
--   moderation_state = 'approved'   —— 审过了。没审过就展示，等于审核形同虚设
--   lifecycle_state = 'published'   —— 已发布，见标准对象 §ST-2 的可见性档位
--   deleted_at IS NULL              —— 没被删除
--   erased_at IS NULL               —— 没被擦除（擦除是逻辑封存，行还在但对外不存在）
--   至少一条 visibility='listed' 的渠道行 —— unlisted / hidden 必须真的不出现
-- 已经归档（archived）或移除（removed）的作品**不在列表里**，但详情页仍可访问（墓碑），
-- 所以这里查询条件与 GetDemoBySlug 故意不同——这不是漏写。

-- name: ListPublishedDemos :many
SELECT d.id, d.slug, d.title, d.cover_url, d.created_at
FROM demos d
WHERE d.moderation_state = 'approved'
  AND d.lifecycle_state = 'published'
  AND d.deleted_at IS NULL
  AND d.erased_at IS NULL
  -- 渠道可见性：unlisted（可直链、不上列表）与 hidden（对外不可见）都必须生效，
  -- 否则发布时选的 visibility 就是个摆设。
  AND EXISTS (
      SELECT 1 FROM demo_channels c
      WHERE c.demo_id = d.id AND c.visibility = 'listed'
  )
ORDER BY d.created_at DESC, d.id DESC
LIMIT $1 OFFSET $2;

-- name: CountPublishedDemos :one
SELECT count(*)
FROM demos d
WHERE d.moderation_state = 'approved'
  AND d.lifecycle_state = 'published'
  AND d.deleted_at IS NULL
  AND d.erased_at IS NULL
  AND EXISTS (
      SELECT 1 FROM demo_channels c
      WHERE c.demo_id = d.id AND c.visibility = 'listed'
  );

-- name: GetDemoBySlug :one
-- 不加可见性过滤：归档/移除的作品要能取出来渲染墓碑页。
-- 「要不要给 404」由调用方按 lifecycle_state / erased_at 判定。
--
-- 多带三列是给「下载」用的：demo_type / single_file 决定下载的是哪个文件，
-- has_visible_channel 决定这张卡片该不该出下载按钮（hidden 渠道的作品对外不可见，
-- 与 /download 端点的判定保持同一口径，否则按钮点了会 404）。
SELECT d.id, d.slug, d.title, d.description, d.cover_url,
       d.lifecycle_state, d.moderation_state, d.created_at, d.updated_at,
       d.deleted_at, d.erased_at,
       COALESCE(r.demo_type, '')::text AS demo_type,
       COALESCE(r.single_file, '')::text AS single_file,
       EXISTS (
           SELECT 1 FROM demo_channels c
           WHERE c.demo_id = d.id AND c.visibility IN ('listed', 'unlisted')
       )::bool AS has_visible_channel
FROM demos d
LEFT JOIN demo_revisions r ON r.id = d.current_revision_id
WHERE d.slug = $1;

-- ===== 投稿写入（Demo 域）=====
-- 这里只做「新作品第一次投稿」需要的三条写。版本追加、审核状态变更、下架等
-- 都是后续端点的事，不在这里提前写。

-- name: CreateDemo :one
-- 刚提交的作品一律 moderation_state='pending' + lifecycle_state='draft'：
-- 待审核、且对外不可见。两个状态是**正交**的两件事，不要合并成一个字段。
INSERT INTO demos (slug, title, description, author_id, moderation_state, lifecycle_state)
VALUES ($1, $2, $3, $4, 'pending', 'draft')
RETURNING id, slug, created_at;

-- name: CreateRevision :one
-- rev_no 写死 1：这是新作品的第一版。追加版本是 updateDemo 的事。
INSERT INTO demo_revisions (
    demo_id, rev_no, title, description, demo_type,
    entry_path, single_file, manifest_hash, file_count, total_bytes, source, created_by
) VALUES ($1, 1, $2, $3, $4, $5, $6, $7, $8, $9, 'upload', $10)
RETURNING id, rev_no;

-- name: SetCurrentRevision :exec
UPDATE demos SET current_revision_id = $2, updated_at = now() WHERE id = $1;

-- name: DeleteDemoForCleanup :exec
-- 仅用于「投稿中途失败」的补偿：这一行是刚刚在同一请求里建的、从未对外可见。
-- **正常删除不走这里**——正常删除是生命周期域的软删 + 墓碑。
--
-- 版本行由 fk_demo_revisions_demo_id76 的 ON DELETE CASCADE 带走（迁移 0006），
-- 不需要先手动删 demo_revisions。demos.current_revision_id 那条边是 SET NULL，
-- 也不妨碍删除。
DELETE FROM demos WHERE id = $1;

-- name: GetDemoForPreview :one
-- 预览托管用：一次取到「这是什么状态、作者是谁、当前是第几版」。
-- author_sub 用 LEFT JOIN + COALESCE 取空串：作者注销后 author_id 会被 SET NULL
-- （迁移 0008），此时作者视图应当消失，而不是让整条预览 500。
-- current_rev_no 同理：还没有版本时是 0，调用方按「没有内容」处理。
SELECT d.id,
       d.slug,
       d.lifecycle_state,
       d.moderation_state,
       d.deleted_at,
       d.erased_at,
       COALESCE(r.rev_no, 0)::int AS current_rev_no,
       r.deleted_at AS revision_deleted_at,
       r.erased_at  AS revision_erased_at,
       COALESCE(u.sub, '')::text AS author_sub
FROM demos d
LEFT JOIN users u ON u.id = d.author_id
LEFT JOIN demo_revisions r ON r.id = d.current_revision_id
WHERE d.slug = $1;

-- name: CountPendingDemosByAuthor :one
-- 投稿配额：一个作者最多同时压多少条待审。
--
-- 没有配额时，登录用户循环 POST 就能把磁盘写满（每次 200 MiB 包 + 最多 500 MiB 解压），
-- 而投稿是全站最贵的写路径。用「待审条数」而不是令牌桶：
-- 它同时防住了「循环投稿」和「攒一堆待审把审核员淹掉」两件事。
SELECT count(*)::int
FROM demos
WHERE author_id = $1
  AND moderation_state = 'pending'
  AND deleted_at IS NULL
  AND erased_at IS NULL;

-- ===== 下载（DEMO-038~041）=====

-- name: GetDemoForDownload :one
-- 下载要一次拿到「该不该给、给哪个文件」两件事，所以与预览分开查：
-- 预览按相对路径取任意文件，下载只认原件（单文件作品是它自己，多文件作品是 source.zip）。
--
-- **单独判渠道可见性**：预览现在不看渠道（已知不一致，另开 issue），
-- 下载必须看——否则 hidden 的作品会变成「藏起来了但谁都能下」。
SELECT d.id,
       d.slug,
       d.lifecycle_state,
       d.moderation_state,
       d.deleted_at,
       d.erased_at,
       COALESCE(r.rev_no, 0)::int AS current_rev_no,
       COALESCE(r.single_file, '')::text AS single_file,
       COALESCE(r.demo_type, '')::text AS demo_type,
       r.deleted_at AS revision_deleted_at,
       r.erased_at  AS revision_erased_at,
       EXISTS (
           SELECT 1 FROM demo_channels c
           WHERE c.demo_id = d.id AND c.visibility IN ('listed', 'unlisted')
       )::bool AS has_visible_channel
FROM demos d
LEFT JOIN demo_revisions r ON r.id = d.current_revision_id
WHERE d.slug = $1;

-- name: BumpDownloadCount :exec
-- demo_stats 之前是空表（连行都没人建），这是第一处写它。
-- UPSERT 而不是先 SELECT 再 UPDATE：两次往返之间别的请求也能插进来，
-- 而这里要的是「原子 +1」，不是「读改写」。
INSERT INTO demo_stats (demo_id, download_count)
VALUES ($1, 1)
ON CONFLICT (demo_id) DO UPDATE
SET download_count = demo_stats.download_count + 1,
    updated_at     = now();
