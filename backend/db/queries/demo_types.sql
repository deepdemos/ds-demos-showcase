-- 作品类型与它允许的上传形态。
--
-- 两条查询刻意长得很像：一条给投稿校验（按 code 取），一条给目录端点（列出全部
-- active）。差异只有 WHERE 与投影，所以谁都不去「顺手复用另一条」——需要不同的
-- 东西就写不同的查询，比在一个函数里分支更好读。

-- name: GetDemoTypeFormats :one
SELECT t.name::text AS name,
       COALESCE(array_agg(f.format ORDER BY f.sort, f.format)
                FILTER (WHERE f.format IS NOT NULL), '{}')::text[] AS formats
FROM demo_types t
LEFT JOIN demo_type_formats f ON f.demo_type_code = t.code
WHERE t.code = sqlc.arg(code)
GROUP BY t.code, t.name;

-- name: ListDemoTypes :many
SELECT t.code::text AS code,
       t.name::text AS name,
       t.sort::int   AS sort,
       COALESCE(array_agg(f.format ORDER BY f.sort, f.format)
                FILTER (WHERE f.format IS NOT NULL), '{}')::text[] AS formats
FROM demo_types t
LEFT JOIN demo_type_formats f ON f.demo_type_code = t.code
WHERE t.status = 'active'
GROUP BY t.code, t.name, t.sort
ORDER BY t.sort, t.code;