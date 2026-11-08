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
-- `q` 是关键词：空串表示不过滤。匹配标题或简介（契约写的就是这两处）。
--
-- 用 ILIKE 而不是全文检索，是刻意的：中文没有空格，pg_trgm 把两字的词
-- （「探针」「回滚」）切不出完整三元组，索引用不上，最后还是回退成扫描。
-- 那不如直接子串匹配——它至少**搜得到**。「搜得快」等数据量真的需要时再谈。
--
-- `q` 在进 SQL 之前已经由 handler 转义过 `\` `%` `_`，所以 `ESCAPE '\'`
-- 之下它就是**字面**子串：搜 `%` 只找含 `%` 的作品，不再等于「全部作品」。
--
-- `matched_in` 告诉前端这条是因为标题命中还是简介命中：只匹配简介时，
-- 结果列表上看到的标题里没有关键词，不标出来就像「搜了但不相关」。
--
-- # 四个筛选参数
--
-- 语义是**维度内 OR、维度间 AND**（`docs/标签域数据结构设计.md` §5.5）。
-- 用 `EXISTS` 而不是 join：一件作品在同维度挂两项时，join 会把列表撑成重复行，
-- 而 `DISTINCT` 之后分页总数就没法用同一条 SQL 算。
--
-- 空数组表示不筛：`cardinality(@genre::text[]) = 0` 时整个 EXISTS 短路。
-- 未收录的 slug 命中 0 行，**不报错**——筛选是查询不是写入。
--
-- 模型筛的是 `current_revision_id` 上的声明（不是已发布版本）：作者改稿换模型后
-- 按旧模型搜不到，这是版本分离的直接后果，与详情页「访客看已发布、作者看当前」
-- 是同一套规则的另一面。
--
-- # 排序（SCH-006）
--
-- `sort` 三个值：`latest`（登记时间新→旧）、`hot`（综合热度，SOC-015）、
-- `random`（按 `seed` 派生稳定随机序）。
--
-- 三种排序写在同一条 ORDER BY 里，靠「不属于本模式的那个 CASE 全是 NULL」退让。
-- 每个 CASE 都显式 `NULLS LAST`：NULL 在 DESC 下默认排最前，
-- 不写就会让 `hot` 模式被 `latest` 的空值顶掉。
--
-- 综合热度取 `demo_stats` 三列（浏览 + 2×下载 + 评分人数）。LEFT JOIN + COALESCE 0：
-- 缺统计行按 0 分算，不能因为「没有统计行」把作品挤出列表——准入条件是可见性。
--
-- 并列按附录 D：热度并列取较晚登记的在前，最后按 id 倒序，保证翻页稳定。
-- `random` 用 `md5(slug:seed)`：同一 seed 下每行位置固定，所以翻页不重不漏（SCH-007）。
SELECT d.id, d.slug, d.title, d.cover_url, d.created_at,
       CASE
           WHEN @q::text = '' THEN 'title'
           WHEN d.title ILIKE '%' || @q::text || '%' ESCAPE '\' THEN 'title'
           ELSE 'description'
       END::text AS matched_in
FROM demos d
LEFT JOIN demo_stats st ON st.demo_id = d.id
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
  AND (
      @q::text = ''
      OR d.title ILIKE '%' || @q::text || '%' ESCAPE '\'
      OR COALESCE(d.description, '') ILIKE '%' || @q::text || '%' ESCAPE '\'
  )
  AND (
      cardinality(@genre::text[]) = 0
      OR EXISTS (
          SELECT 1 FROM demo_classifications dc
          JOIN classifications cl ON cl.id = dc.classification_id
          WHERE dc.demo_id = d.id AND cl.dimension = 'genre'
            AND cl.slug = ANY(@genre::text[])
      )
  )
  AND (
      cardinality(@technique::text[]) = 0
      OR EXISTS (
          SELECT 1 FROM demo_classifications dc
          JOIN classifications cl ON cl.id = dc.classification_id
          WHERE dc.demo_id = d.id AND cl.dimension = 'technique'
            AND cl.slug = ANY(@technique::text[])
      )
  )
  AND (
      cardinality(@subject::text[]) = 0
      OR EXISTS (
          SELECT 1 FROM demo_classifications dc
          JOIN classifications cl ON cl.id = dc.classification_id
          WHERE dc.demo_id = d.id AND cl.dimension = 'subject'
            AND cl.slug = ANY(@subject::text[])
      )
  )
  AND (
      cardinality(@model::text[]) = 0
      OR EXISTS (
          SELECT 1 FROM demo_revision_models rm
          JOIN models m ON m.id = rm.model_id
          WHERE rm.revision_id = d.current_revision_id
            AND m.slug = ANY(@model::text[])
      )
  )
ORDER BY
    CASE WHEN @sort::text = 'random' THEN md5(d.slug || ':' || @seed::text) END,
    CASE WHEN @sort::text = 'hot'
         THEN COALESCE(st.view_count, 0) + 2 * COALESCE(st.download_count, 0)
              + COALESCE(st.rating_count, 0) END DESC NULLS LAST,
    CASE WHEN @sort::text = 'latest' THEN d.created_at END DESC NULLS LAST,
    d.created_at DESC, d.id DESC
LIMIT sqlc.arg(row_limit) OFFSET sqlc.arg(row_offset);

-- name: CountPublishedDemos :one
-- 与 ListPublishedDemos 的 WHERE 必须逐字一致（含 q 与四个筛选的全部条件），
-- 否则「共 N 个作品」与列表条数会对不上，分页也会算错页数。
SELECT count(*)
FROM demos d
WHERE d.published_revision_id IS NOT NULL
  AND d.lifecycle_state = 'published'
  AND d.deleted_at IS NULL
  AND d.erased_at IS NULL
  -- 同上：这里是「上列表」判据的第二处副本，与 ListPublishedDemos 的 WHERE
  -- 必须逐字一致（含这行注释外的条件）。
  AND EXISTS (
      SELECT 1 FROM demo_channels c
      WHERE c.demo_id = d.id AND c.visibility = 'listed'
  )
  AND (
      @q::text = ''
      OR d.title ILIKE '%' || @q::text || '%' ESCAPE '\'
      OR COALESCE(d.description, '') ILIKE '%' || @q::text || '%' ESCAPE '\'
  )
  AND (
      cardinality(@genre::text[]) = 0
      OR EXISTS (
          SELECT 1 FROM demo_classifications dc
          JOIN classifications cl ON cl.id = dc.classification_id
          WHERE dc.demo_id = d.id AND cl.dimension = 'genre'
            AND cl.slug = ANY(@genre::text[])
      )
  )
  AND (
      cardinality(@technique::text[]) = 0
      OR EXISTS (
          SELECT 1 FROM demo_classifications dc
          JOIN classifications cl ON cl.id = dc.classification_id
          WHERE dc.demo_id = d.id AND cl.dimension = 'technique'
            AND cl.slug = ANY(@technique::text[])
      )
  )
  AND (
      cardinality(@subject::text[]) = 0
      OR EXISTS (
          SELECT 1 FROM demo_classifications dc
          JOIN classifications cl ON cl.id = dc.classification_id
          WHERE dc.demo_id = d.id AND cl.dimension = 'subject'
            AND cl.slug = ANY(@subject::text[])
      )
  )
  AND (
      cardinality(@model::text[]) = 0
      OR EXISTS (
          SELECT 1 FROM demo_revision_models rm
          JOIN models m ON m.id = rm.model_id
          WHERE rm.revision_id = d.current_revision_id
            AND m.slug = ANY(@model::text[])
      )
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
           SELECT 1 FROM demo_visible_channels c
           WHERE c.demo_id = d.id
       )::bool AS has_visible_channel,
       -- 业务统计：详情页要展示「浏览 N 次 · 下载 M 次」。
       -- 用 COALESCE 而不是 JOIN 后的直接列：demo_stats 是懒建的，
       -- 一件从没人看过的新作品在这里根本没有行。
       COALESCE(st.view_count, 0)::bigint AS view_count,
       COALESCE(st.download_count, 0)::bigint AS download_count,
       -- 作者（DEMO-042 第 4 项）：昵称优先，作者没设昵称时回退到用户名。
       -- 展示名在 SQL 里就兜好，别让三个前端各自实现一遍「为空怎么办」。
       COALESCE(pf.display_name, '')::text AS author_display_name,
       COALESCE(pf.username, '')::text AS author_username,
       -- 发布时间（第 7 项）：demo_channels.published_at 是发布动作写的、
       -- 且 `UpsertDemoChannel` 用 COALESCE 保留第一次的值。
       -- 一个作品可以挂多个渠道，取最早的一次——「它什么时候第一次被发出去」。
       -- 用子查询而不是 join：渠道有零到多行，join 会把详情行复制成多行。
       --
       -- 只算**对外可见**的渠道：hidden 是「内部预览，对外不可见」，
       -- 把它算进来会让发布时间提前几个月（先发 hidden、确认后再上 listed 正是
       -- 这个可见性存在的理由）。判据本身在视图 demo_visible_channels 里，
       -- 这里只问视图，不再复述枚举。
       (SELECT min(c.published_at)::timestamptz
          FROM demo_visible_channels c
         WHERE c.demo_id = d.id)::timestamptz AS published_at,
       -- 两条版本轴的 id（模型声明挂在 revision 上，要按视图选一条取）。
       d.published_revision_id, d.current_revision_id
FROM demos d
LEFT JOIN users u ON u.id = d.author_id
LEFT JOIN demo_revisions r ON r.id = d.published_revision_id
LEFT JOIN demo_revisions cr ON cr.id = d.current_revision_id
LEFT JOIN demo_stats st ON st.demo_id = d.id
-- accounts 只用来接到 profiles：profiles 的主键是 accounts.id，不是 users.id。
-- users 与 accounts 以 sub 对应（同一个 IdP 主体两条投影，见身份域 §4）。
LEFT JOIN accounts ac ON ac.sub = u.sub
LEFT JOIN profiles pf ON pf.account_id = ac.id
WHERE d.slug = $1;

-- ===== 投稿写入（Demo 域）=====
-- 这里只做「新作品第一次投稿」需要的三条写。版本追加、审核状态变更、下架等
-- 都是后续端点的事，不在这里提前写。

-- name: CreateDemo :one
-- 刚提交的作品一律 moderation_state='pending' + lifecycle_state='draft'：
-- 待审核、且对外不可见。两个状态是**正交**的两件事，不要合并成一个字段。
--
-- `idempotency_key` 是客户端给的「这次提交的编号」，可以不给（NULL）。
-- 给了就靠 `uq_demos_idempotency_key` 保证同一个作者的同一个 key 只落一行：
-- 脚本传 200 MiB 传到一半断了重传，不该变成两条一模一样的作品。
INSERT INTO demos (slug, title, description, author_id, moderation_state, lifecycle_state, idempotency_key)
VALUES ($1, $2, $3, $4, 'pending', 'draft', sqlc.narg('idempotency_key'))
RETURNING id, slug, created_at;

-- name: FindDemoByIdempotencyKey :one
-- 重放用的查询：同一个作者的同一个 key 已经落过一行，就把**当初那条**的结果还回去。
--
-- 按 author_id 限定，不是因为 key 会在作者之间撞（key 由客户端生成），
-- 而是因为不限定就等于「任何人报出别人的 key 就能读到别人作品的 slug」——
-- 幂等查询必须自己带权限边界，不能指望调用方记得加。
--
-- 不看 deleted_at / erased_at：重放要回答的是「这件事我是不是已经做过」，
-- 作品后来被删了也还是做过。真删干净（擦除）之后这一行照样在，
-- 只是 slug 指向的东西对外不存在了——那正是事实。
SELECT d.slug,
       d.moderation_state,
       d.lifecycle_state,
       COALESCE(r.rev_no, 1)::int AS rev_no
FROM demos d
LEFT JOIN demo_revisions r ON r.id = d.current_revision_id
WHERE d.author_id = $1 AND d.idempotency_key = $2;

-- name: CreateRevision :one
-- rev_no 写死 1：这是新作品的第一版。追加版本是 updateDemo 的事。
INSERT INTO demo_revisions (
    demo_id, rev_no, title, description, demo_type,
    entry_path, single_file, manifest_hash, file_count, total_bytes, source, created_by
) VALUES ($1, 1, $2, $3, $4, $5, $6, $7, $8, $9, 'upload', $10)
RETURNING id, rev_no;

-- name: SetCurrentRevision :exec
UPDATE demos SET current_revision_id = $2, updated_at = GREATEST(now(), created_at) WHERE id = $1;

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
    updated_at          = GREATEST(now(), created_at)
WHERE id = $1;

-- name: DeleteDemoForCleanup :exec
-- 仅用于「投稿中途失败」的补偿：这一行是刚刚在同一请求里建的、从未对外可见。
-- **正常删除不走这里**——正常删除是生命周期域的软删 + 墓碑。
--
-- 版本行由 fk_demo_revisions_demo_id76 的 ON DELETE CASCADE 带走（迁移 0006），
-- 不需要先手动删 demo_revisions。demos.current_revision_id 那条边是 SET NULL，
-- 也不妨碍删除。
DELETE FROM demos WHERE id = $1;

-- name: GetDemoForTimeline :one
-- 作品动态的可见性判定：谁能读、读的是哪一行。
--
-- 只取「作者是谁」与「有没有被擦除」：动态的可见性判定**只**看这两件事
--（按 D1 的裁决：审核员不给、墓碑页不给）——不掺 lifecycle 与渠道，
-- 因为那是「内容能不能看」的口径，与「你能否读到自己的改动史」不是同一个问题。
SELECT d.id,
       COALESCE(ac.sub, '')::text AS author_sub,
       d.erased_at
FROM demos d
JOIN users u ON u.id = d.author_id
LEFT JOIN accounts ac ON ac.sub = u.sub
WHERE d.slug = @slug::text;

-- name: ListDemoTimeline :many
-- 写给人看的经过（Demo 域 §3.11）。`kind` 的取值见 `ck_timeline_kind`。
--
-- 操作人显示名走 users → accounts.sub → profiles：`demo_timeline.actor_id` 存的是
-- users.id（外键指向 users），而显示名在 profiles 上、挂在 accounts 下。
-- 账号被删时显示名为空，那是事实，不要用「系统」掩盖。
--
-- 限 100 条不分页：单件作品的动作数量级是几十（见契约描述）。
SELECT t.kind,
       COALESCE(t.message, '')::text AS message,
       t.created_at,
       COALESCE(pf.display_name, '')::text AS actor_name
FROM demo_timeline t
LEFT JOIN users u ON u.id = t.actor_id
LEFT JOIN accounts a ON a.sub = u.sub
LEFT JOIN profiles pf ON pf.account_id = a.id
WHERE t.demo_id = @demo_id::bigint
ORDER BY t.created_at DESC, t.id DESC
LIMIT 100;

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
       -- 入口文件：单文件作品可能是 `index.svg`，zip 包也可能是 `dist/index.html`。
       -- 预览在 `rel` 为空时**必须**用它；硬编码 `index.html` 会让这两类作品
       -- 打开就是 404（真实事故：上传一张 SVG，预览页打不开）。
       COALESCE(r.entry_path, '')::text AS current_entry_path,
       COALESCE(pr.entry_path, '')::text AS published_entry_path,
       COALESCE(u.sub, '')::text AS author_sub,
       -- 渠道可见性：与 /download 同一口径（listed 或 unlisted 才算对外可见）。
       -- 之前预览完全不判这一列，导致 hidden 的作品「从列表里藏起来了，但知道直链的人
       -- 仍然能看完整内容」。下载那一轮把这个口子堵了一半（下载判了），预览没动。
       --
       -- 注意：对外可见的**内容来源**是 published 那个版本，不是这一列。
       EXISTS (
           SELECT 1 FROM demo_visible_channels c
           WHERE c.demo_id = d.id
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
-- **单独判渠道可见性**：与预览同一个口径（`GetDemoForPreview` 里的 EXISTS），
-- 两处不能一个判一个不判——否则 hidden 的作品会变成「藏起来了但谁都能下」。
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
           SELECT 1 FROM demo_visible_channels c
           WHERE c.demo_id = d.id
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

-- ===== 浏览计数（DEMO-041）=====

-- name: RecordDemoView :one
-- 一次浏览能不能算，全在这一条里判完：
--   1) 作品得是对外可读的（有对外版本、已发布、未删未擦）——草稿不该有浏览量；
--   2) 同一个人同一个自然日只算一次（主键冲突就不插入，也就不会走到 +1）；
--   3) 去重行与计数必须在**同一条语句**里：分两条的话，第一步成功、第二步失败，
--      这个人今天就再也加不上了（去重行在，永远是冲突）——那次浏览永久丢失。
--
-- **故意不判渠道visible性**：浏览计的是「有人来看了」，不是「它出现在哪里」。
-- 渠道是 hidden 的作品，访客的详情请求本来就会被详情端点挡成 404，
-- 到不了这里；而能拿到 slug 的人直接 POST 也只是给自己的记录加一行，一天一次封顶。
--
-- 外层 INSERT ... SELECT FROM ins 读的是 CTE 的 RETURNING，不是表——
-- 「数据修改型 CTE 之间看不见彼此的写入」说的是读表，读 CTE 的返回值是允许的。
-- 返回 0 行 = 今天已经看过（或作品不可见），调用方据此判断这次算不算。
WITH ins AS (
    INSERT INTO demo_views (demo_id, viewer_key, viewed_on)
    SELECT d.id, sqlc.arg(viewer_key)::text, current_date
    FROM demos d
    WHERE d.slug = sqlc.arg(slug)::text
      AND d.lifecycle_state = 'published'
      AND d.published_revision_id IS NOT NULL
      AND d.deleted_at IS NULL
      AND d.erased_at IS NULL
    ON CONFLICT (demo_id, viewer_key, viewed_on) DO NOTHING
    RETURNING demo_id
)
INSERT INTO demo_stats (demo_id, view_count)
SELECT demo_id, 1 FROM ins
ON CONFLICT (demo_id) DO UPDATE
SET view_count = demo_stats.view_count + 1,
    updated_at  = now()
RETURNING demo_id;

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

-- ===== 作品生命周期（上下架 / 软删 / 恢复）=====
--
-- 与「改稿」「回滚」共用同一张 demos 行，但动的是**另两层**：
-- 可见性层（lifecycle_state）与删除标记层（deleted_at）。两层是笛卡尔积，
-- 所以软删不改 lifecycle_state，恢复只清 deleted_at（生命周期 §4.3）。
--
-- 留痕由 domain 层在同一个事务里写两条：
--   下架 / 重新上架 -> demo_timeline(kind='archive'/'restore') + audit_log(action='status_set')
--   软删 / 恢复删除 -> 只有 audit_log(action='delete'/'restore')，不进时间线（对外不存在）
-- 判据与理由见 backend/internal/domain/demo/lifecycle.go 文件头。

-- name: GetDemoForLifecycle :one
-- 一次拿齐「能不能动」需要的全部状态。判定放在领域层，SQL 只负责取。
SELECT d.id, d.slug, d.author_id, d.lifecycle_state, d.moderation_state,
       d.published_revision_id, d.deleted_at, d.erased_at
FROM demos d
WHERE d.slug = $1;

-- name: TransitionDemoLifecycle :execrows
-- 生命周期只有这一个写入口：目标状态与前置状态都由调用方给，条件在同一句里。
--
-- 为什么不拆成 ArchiveDemo / UnarchiveDemo 两条：那会让「哪些跃迁合法」变成
-- 两个文件位置的事实，接「平台移除」「申诉恢复」时就是第三、第四份。
-- 现在合法性由 WHERE 的 lifecycle_state = @from_state 表达，唯一；0 行 = 跃迁不合法。
UPDATE demos
SET lifecycle_state = @to_state::text, updated_at = GREATEST(now(), created_at)
WHERE id = @demo_id
  AND lifecycle_state = @from_state::text
  AND deleted_at IS NULL
  AND erased_at IS NULL;

-- name: SoftDeleteDemo :exec
-- 软删只写删除标记层，lifecycle_state 一个字节不动（生命周期 §4.3）。
UPDATE demos
SET deleted_at = GREATEST(now(), created_at), delete_reason = $2,
    updated_at = GREATEST(now(), created_at)
WHERE id = $1 AND deleted_at IS NULL AND erased_at IS NULL;

-- name: RestoreDeletedDemo :exec
-- 恢复 = 清空 deleted_at。作品自动回到删除前的档位，不需要额外的「原状态」列。
UPDATE demos
SET deleted_at = NULL, delete_reason = '', updated_at = GREATEST(now(), created_at)
WHERE id = $1 AND deleted_at IS NOT NULL AND erased_at IS NULL;

-- name: GetDemoGcWindow :one
-- 恢复窗口的唯一判据：这条作品入队时算出的 purge_after（不是「当前配置的窗口」重算）。
-- 读不到行 = 没入过队 = 没有窗口可判，调用方按「不可恢复」处理。
SELECT q.purge_after, q.purged_at, q.abandoned_at
FROM gc_queue q
WHERE q.kind = 'demo_dir' AND q.object_key = @object_key::text
ORDER BY q.id DESC
LIMIT 1;

-- name: EnqueueDemoGc :exec
-- kind 只能是 demo_dir（gc_kinds 三选一里唯一与作品目录对应的）。
-- object_key 用作品目录前缀而不是 slug：GC 要删的是一整个前缀，
-- 而 slug 本身在旧数据里也常作目录名，两者拼起来才无歧义。
INSERT INTO gc_queue (kind, object_key, entity_type, entity_id, reason, purge_after)
VALUES ('demo_dir', @object_key::text, 'demo', @entity_id::bigint, 'delete', @purge_after::timestamptz)
ON CONFLICT (kind, object_key) DO UPDATE
SET purge_after = EXCLUDED.purge_after,
    entity_id   = EXCLUDED.entity_id,
    purged_at   = NULL,
    abandoned_at = NULL;

-- name: DeleteDemoGcRow :exec
-- 恢复时把队列行删掉：行在，就代表「这个前缀还要被清掉」。
-- 设计里写明「恢复会删掉本行，故『删 → 恢复 → 再删』走 INSERT 不走 upsert」。
DELETE FROM gc_queue
WHERE kind = 'demo_dir' AND object_key = @object_key::text;
