-- 作品列表与详情。
--
-- 「公开可见」的判定口径（列表、详情、预览、下载四处必须一致，改一处就要改其余三处）：
--   published_revision_id IS NOT NULL —— 有对外版本。**不再看审核态**：
--                                     审核态描述的是「当前版本」，一件已发布作品在
--                                     复审期间审核态是 pending，但对外内容仍在服务
--                                     （迁移 0014 的版本分离，理由见那个文件头）
--   lifecycle_state = 'published'   —— 已发布，见标准对象 §ST-2 的可见性档位
--   deleted_at IS NULL              —— 没被删除
--   erased_at IS NULL               —— 没被擦除（擦除是逻辑封存，行还在但对外不存在）
--   至少一条 visibility='listed' 的渠道行 —— unlisted / hidden 必须真的不出现
-- 已经归档（archived）或移除（removed）的作品**不在列表里**，但详情页仍可访问（墓碑），
-- 所以这里查询条件与 GetDemoBySlug 故意不同——这不是漏写。

-- name: ListPublishedDemos :many
SELECT d.id, d.slug, d.title, d.cover_url, d.created_at
FROM demos d
WHERE d.published_revision_id IS NOT NULL
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
WHERE d.published_revision_id IS NOT NULL
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
       -- 类型取**对外版本**的：详情页对所有人展示的是一份内容，
       -- 作者草稿还没过审时也不该把「新版本的类型」泄露出去。
       COALESCE(r.demo_type, '')::text AS demo_type,
       COALESCE(r.single_file, '')::text AS single_file,
       (d.published_revision_id IS NOT NULL)::bool AS has_published_revision,
       COALESCE(r.rev_no, 0)::int AS published_rev_no,
       -- 作者视图要用的三列（迁移 0014 之后「当前版本」与「对外版本」是两条时间轴）：
       -- 作者在自己的详情页里看到的应当是**他刚提交的那一版**，不是对外那一版。
       -- author_sub 用 COALESCE 取空串：作者注销后 author_id 被 SET NULL，
       -- 此时作者视图就该消失，而不是让整条详情 500。
       COALESCE(u.sub, '')::text AS author_sub,
       COALESCE(cr.title, '')::text AS current_title,
       COALESCE(cr.description, '')::text AS current_description,
       COALESCE(cr.demo_type, '')::text AS current_demo_type,
       COALESCE(cr.rev_no, 0)::int AS current_rev_no,
       EXISTS (
           SELECT 1 FROM demo_channels c
           WHERE c.demo_id = d.id AND c.visibility IN ('listed', 'unlisted')
       )::bool AS has_visible_channel
FROM demos d
LEFT JOIN users u ON u.id = d.author_id
LEFT JOIN demo_revisions r ON r.id = d.published_revision_id
LEFT JOIN demo_revisions cr ON cr.id = d.current_revision_id
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

-- ===== 更新作品（追加版本）=====

-- name: GetDemoForEdit :one
-- 更新时要把「现在这一版是什么样」一次读全：类型、入口、清单统计。
-- 只换文字不换文件时，新版本直接继承这几项，不必再读一遍清单。
SELECT d.id,
       d.slug,
       d.author_id,
       d.lifecycle_state,
       d.moderation_state,
       d.deleted_at,
       d.erased_at,
       d.published_revision_id IS NOT NULL AS has_published_revision,
       d.current_revision_id,
       COALESCE(r.rev_no, 0)::int AS current_rev_no,
       COALESCE(r.demo_type, '')::text AS demo_type,
       COALESCE(r.single_file, '')::text AS single_file,
       COALESCE(r.entry_path, '')::text AS entry_path,
       COALESCE(r.manifest_hash, '')::text AS manifest_hash,
       COALESCE(r.file_count, 0)::bigint AS file_count,
       COALESCE(r.total_bytes, 0)::bigint AS total_bytes
FROM demos d
LEFT JOIN demo_revisions r ON r.id = d.current_revision_id
WHERE d.slug = $1;

-- name: NextRevisionNo :one
-- 版本号由数据库算，不由应用层「读最大值再加一」：并发的两次更新会读到同一个最大值，
-- 然后一个撞唯一索引、另一个成功——撞的那个白做了一次上传。
SELECT (COALESCE(MAX(rev_no), 0) + 1)::int AS rev_no
FROM demo_revisions
WHERE demo_id = $1;

-- name: AppendRevision :one
-- 与 CreateRevision 的差别只有 rev_no：那一版写死 1（新作品的第一版），
-- 这一版由 NextRevisionNo 给出。source 仍是 'upload'——CHECK 里没有 'edit'，
-- 而「这是编辑」这件事由 demo_timeline.kind='edit' 记，不靠这一列区分。
INSERT INTO demo_revisions (
    demo_id, rev_no, title, description, demo_type,
    entry_path, single_file, manifest_hash, file_count, total_bytes, source, created_by
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, 'upload', $11)
RETURNING id, rev_no;

-- name: CopyRevisionFiles :exec
-- 「只改文字不换文件」时把上一版的清单整表复制过去（同一批 blob，零拷贝）。
-- ON CONFLICT 是防御性的：新版本号不该有行，但复制语义下宁可重复执行无害。
INSERT INTO demo_revision_files (revision_id, path, blob_hash, size, mime)
SELECT sqlc.arg(target_revision_id), f.path, f.blob_hash, f.size, f.mime
FROM demo_revision_files f
WHERE f.revision_id = sqlc.arg(source_revision_id)
ON CONFLICT ON CONSTRAINT uq_manifest_path DO NOTHING;

-- name: SetCurrentRevisionForEdit :exec
-- 追加版本后：当前版本指向新版，审核态回到 pending（复审），**对外版本不动**。
-- 这是「改稿期间线上作品照常服务」的全部实现——没有它，作者一提交更新，
-- 自己作品的新内容就绕过审核直接对外了。
UPDATE demos
SET current_revision_id = $2,
    moderation_state    = 'pending',
    updated_at          = now()
WHERE id = $1;

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
       -- 对外版本：公开访客读这一版，作者与审核员读 current 那版（迁移 0014）。
       -- 两列都给出来，由调用方按「谁在看」选，而不是让 SQL 猜。
       (d.published_revision_id IS NOT NULL)::bool AS has_published_revision,
       COALESCE(pr.rev_no, 0)::int AS published_rev_no,
       pr.deleted_at AS published_deleted_at,
       pr.erased_at  AS published_erased_at,
       COALESCE(u.sub, '')::text AS author_sub,
       -- 渠道可见性：与 /download 同一口径（listed 或 unlisted 才算对外可见）。
       -- 之前预览完全不判这一列，导致 hidden 的作品「从列表里藏起来了，但知道直链的人
       -- 仍然能看完整内容」。下载那一轮把这个口子堵了一半（下载判了），预览没动。
       --
       -- 注意：对外可见的**内容来源**是 published 那个版本，不是这一列。
       EXISTS (
           SELECT 1 FROM demo_channels c
           WHERE c.demo_id = d.id AND c.visibility IN ('listed', 'unlisted')
       )::bool AS has_visible_channel
FROM demos d
LEFT JOIN users u ON u.id = d.author_id
LEFT JOIN demo_revisions r ON r.id = d.current_revision_id
LEFT JOIN demo_revisions pr ON pr.id = d.published_revision_id
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
       -- 下载给的是**对外版本**（迁移 0014）。作者的新草稿还没过审时，
       -- 公开访客下到的必须仍是线上那一版，否则「审核」对下载就没有约束力。
       COALESCE(r.rev_no, 0)::int AS published_rev_no,
       COALESCE(r.single_file, '')::text AS single_file,
       COALESCE(r.demo_type, '')::text AS demo_type,
       r.deleted_at AS revision_deleted_at,
       r.erased_at  AS revision_erased_at,
       EXISTS (
           SELECT 1 FROM demo_channels c
           WHERE c.demo_id = d.id AND c.visibility IN ('listed', 'unlisted')
       )::bool AS has_visible_channel
FROM demos d
LEFT JOIN demo_revisions r ON r.id = d.published_revision_id
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

-- ===== 版本历史与回滚（设计 §7.3）=====

-- name: ListDemoRevisions :many
-- 作者看自己的、审核员看任意作品。授权不在 SQL 里做（数据库不知道该是谁在调用），
-- 这里只负责「按作品把所有版本列出来」。
--
-- `is_current` / `is_published` 用两个指针直接比，不在应用层再推一遍：
-- 这两个布尔就是「作者在改哪一版」「访客在看哪一版」的唯一事实源。
SELECT r.rev_no,
       COALESCE(r.title, '')::text        AS title,
       r.demo_type,
       r.source,
       COALESCE(r.file_count, 0)::bigint  AS file_count,
       COALESCE(r.total_bytes, 0)::bigint AS total_bytes,
       r.created_at,
       (r.deleted_at IS NOT NULL)::bool   AS deleted,
       (r.erased_at IS NOT NULL)::bool    AS erased,
       -- 必须包 COALESCE：`r.id = NULL` 的结果是 NULL 而不是 false，
       -- 扫进 Go 的 bool 会直接报 `cannot scan NULL into *bool`。
       COALESCE(r.id = d.current_revision_id, false)::bool   AS is_current,
       COALESCE(r.id = d.published_revision_id, false)::bool AS is_published
FROM demo_revisions r
JOIN demos d ON d.id = r.demo_id
WHERE d.slug = $1
ORDER BY r.rev_no DESC;

-- name: GetDemoForRestore :one
-- 回滚要一次拿到三件事：目标版本的内容元数据、作品的归属与生命周期、目标版本本身还能不能用。
-- 擦除过的版本不能作为回滚目标——它的内容列已经清空了，回滚过去等于产出一个空版本。
SELECT d.id AS demo_id,
       d.slug,
       d.author_id,
       d.lifecycle_state,
       d.moderation_state,
       d.deleted_at,
       d.erased_at,
       d.current_revision_id,
       r.id  AS revision_id,
       r.rev_no,
       COALESCE(r.title, '')::text       AS title,
       COALESCE(r.description, '')::text AS description,
       r.demo_type,
       r.entry_path,
       r.single_file,
       r.manifest_hash,
       COALESCE(r.file_count, 0)::bigint  AS file_count,
       COALESCE(r.total_bytes, 0)::bigint AS total_bytes,
       r.deleted_at AS revision_deleted_at,
       r.erased_at  AS revision_erased_at
FROM demos d
JOIN demo_revisions r ON r.demo_id = d.id
WHERE d.slug = $1 AND r.rev_no = $2;

-- name: AppendRollbackRevision :one
-- 与 AppendRevision 的唯一差别是 source 写 'rollback'（CHECK 允许 upload / url_import / rollback）。
-- 「回滚 = 追加一个新版本指向旧清单」——历史一行不删，这也是它能失败安全的原因：
-- 只要新版本没建出来，原来的对外版本一个字节都没动。
INSERT INTO demo_revisions (
    demo_id, rev_no, title, description, demo_type,
    entry_path, single_file, manifest_hash, file_count, total_bytes, source, created_by
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, 'rollback', $11)
RETURNING id, rev_no;
