-- 公开作者页的读路径（#425）。
--
-- 只读 profiles / accounts / avatars，不读 sessions / credentials。
-- 作品计数与列表都走 demo_public_demos 视图：公开性的判据只有那一处。

-- name: GetPublicProfileByUsername :one
-- 按用户名大小写不敏感查找（uq_profiles_username 建在 lower(username) 上）。
-- 只出 active 账号：pending（未验证）/ suspended / banned / deleted 都不是公开作者。
-- demoCount 与列表同源（demo_public_demos）。
SELECT p.username,
       p.display_name,
       p.bio,
       a.created_at AS joined_at,
       av.url       AS avatar_url,
       (
           SELECT count(*)
           FROM demo_public_demos dv
           JOIN demos d ON d.id = dv.demo_id
           JOIN users u ON u.id = d.author_id
           WHERE u.sub = a.sub
       )::bigint AS demo_count
FROM profiles p
JOIN accounts a ON a.id = p.account_id
LEFT JOIN avatars av ON av.account_id = p.account_id
WHERE lower(p.username) = lower(sqlc.arg(username)::text)
  AND a.status = 'active';

-- name: ListPublicDemosByAuthor :many
-- 作者页的作品列表：与作品库同一份公开判据（demo_public_demos）。
SELECT d.slug,
       d.title,
       d.cover_url,
       d.created_at
FROM demo_public_demos dv
JOIN demos d ON d.id = dv.demo_id
JOIN users u ON u.id = d.author_id
JOIN accounts a ON a.sub = u.sub
JOIN profiles p ON p.account_id = a.id
WHERE lower(p.username) = lower(sqlc.arg(username)::text)
ORDER BY d.created_at DESC, d.id DESC
LIMIT sqlc.arg(row_limit) OFFSET sqlc.arg(row_offset);

-- name: CountPublicDemosByAuthor :one
-- 与 ListPublicDemosByAuthor 的 WHERE 逐字一致：total 与列表条数对不上时页数会算错。
SELECT count(*)::bigint AS total
FROM demo_public_demos dv
JOIN demos d ON d.id = dv.demo_id
JOIN users u ON u.id = d.author_id
JOIN accounts a ON a.sub = u.sub
JOIN profiles p ON p.account_id = a.id
WHERE lower(p.username) = lower(sqlc.arg(username)::text);
