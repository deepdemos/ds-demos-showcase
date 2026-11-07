-- name: UpsertDemoRating :one
-- 一票的写入：同一个人再打一次是改分（`ON CONFLICT ... DO UPDATE`），不是追加。
--
-- 作品的可见性判据与详情、浏览计数同源：有对外版本、已发布、未删未擦。
-- 草稿不该有评分——它连详情都打不开。
--
-- 不判 `author_id`：作者可以给自己打分（`rate-self B`）。拦它的收益只有
-- 「作者不能自夸」这一点面子，代价是要额外解释「为什么作者不能参与」。
INSERT INTO demo_ratings (demo_id, user_id, rater_key, score)
SELECT d.id, sqlc.narg(user_id)::bigint, sqlc.arg(rater_key)::text, sqlc.arg(score)::int
FROM demos d
WHERE d.slug = sqlc.arg(slug)::text
  AND d.lifecycle_state = 'published'
  AND d.published_revision_id IS NOT NULL
  AND d.deleted_at IS NULL
  AND d.erased_at IS NULL
ON CONFLICT ON CONSTRAINT uq_rating_per_rater DO UPDATE
    SET score = EXCLUDED.score,
        -- 时间夹紧：WSL 的系统时钟会回拨（实测六次），这里裸写 now() 会让
        -- ck_rating_time 偶发 503。与 demos 的时间列同一处置。
        updated_at = GREATEST(now(), demo_ratings.created_at)
RETURNING demo_id;

-- name: DeleteDemoRating :one
-- 撤销一票。没打过时返回 0 行——调用方把它当「意图已达成」，不是错误
-- （`DELETE` 本来就是幂等的）。
DELETE FROM demo_ratings r
USING demos d
WHERE r.demo_id = d.id
  AND d.slug = sqlc.arg(slug)::text
  AND r.rater_key = sqlc.arg(rater_key)::text
  AND d.deleted_at IS NULL
  AND d.erased_at IS NULL
RETURNING r.demo_id;

-- name: DemoIDForRating :one
-- 只取 id：撤销时如果作品不可见要能和「没打过」区分开（前者 404，后者 200）。
SELECT d.id
FROM demos d
WHERE d.slug = sqlc.arg(slug)::text
  AND d.lifecycle_state = 'published'
  AND d.published_revision_id IS NOT NULL
  AND d.deleted_at IS NULL
  AND d.erased_at IS NULL;

-- name: RefreshDemoRatingStats :one
-- 从 `demo_ratings` 整表重算这一件的汇总，写进 `demo_stats`。
--
-- 为什么是重算而不是增量：改分与撤销都会让「总和」减少，增量写法要记住旧分，
-- 而重算只有一条路径，错了也能靠再跑一次恢复。票数在几百这个量级，
-- `ix_ratings_demo` 足够。
--
-- 为什么不在一条语句里跟 INSERT 合并：数据修改型 CTE **互相看不见对方的写入**，
-- 聚合 CTE 读 `demo_ratings` 读到的还是插入前的快照——统计会永远落后一票。
-- 所以这两步必须各自成语句（同一事务内，后一条看得见前一条）。
WITH agg AS (
    SELECT COALESCE(sum(r.score), 0)::bigint                                   AS s,
           count(r.id)::bigint                                                 AS c,
           COALESCE(round(avg(r.score), 2), 0)::numeric(3, 2)                  AS a,
           count(*) FILTER (WHERE r.score = 5)::bigint                         AS god,
           count(*) FILTER (WHERE r.score = 1)::bigint                         AS ghost
    FROM demo_ratings r
    WHERE r.demo_id = sqlc.arg(demo_id)::bigint
)
INSERT INTO demo_stats (demo_id, rating_sum, rating_count, rating_avg, rating_god, rating_ghost)
SELECT sqlc.arg(demo_id)::bigint, s, c, a, god, ghost FROM agg
ON CONFLICT (demo_id) DO UPDATE
    SET rating_sum = EXCLUDED.rating_sum,
        rating_count = EXCLUDED.rating_count,
        rating_avg = EXCLUDED.rating_avg,
        rating_god = EXCLUDED.rating_god,
        rating_ghost = EXCLUDED.rating_ghost,
        -- demo_stats 没有时间顺序约束（它没有 created_at），裸 now() 是安全的。
        updated_at = now()
RETURNING rating_avg, rating_count;

-- name: MyDemoRating :one
-- 当前调用者给这件作品打的分。`pgx.ErrNoRows` = 没打过。
SELECT r.score
FROM demo_ratings r
JOIN demos d ON d.id = r.demo_id
WHERE d.slug = sqlc.arg(slug)::text
  AND r.rater_key = sqlc.arg(rater_key)::text;

-- name: RatingStatsBySlug :one
-- 只读汇总。没有行（从未被评分过）时返回 ErrNoRows，调用方当 0 分 0 票处理。
SELECT COALESCE(s.rating_avg, 0)::numeric(3, 2) AS rating_avg,
       COALESCE(s.rating_count, 0)::bigint      AS rating_count
FROM demos d
LEFT JOIN demo_stats s ON s.demo_id = d.id
WHERE d.slug = sqlc.arg(slug)::text;
