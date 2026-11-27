-- 上游条目的公开搜索（#589）：模型选择器的数据源。
--
-- 查的是本地镜像 `model_catalog_items`，不是实时问上游：models.dev 只提供整包 JSON
-- （约 10MB），没有搜索接口；上游不可用时本站照常工作（模型域 §4.6.1）。
--
-- 「已收录 → 站内 slug」与导入页的 `ListCatalogItems.imported` 是**同一个判定**：
-- `model_offerings` 里有没有 `(authority=source, provider_id, external_id)` 这条认领行。
-- 这里多做一步 join 把 `models.slug` 带出来（选择器要显示「已收录 → slug」），
-- 判定本身不重复实现：Go 侧也不重算，选择器只读这个字段。
--
-- 不过滤 `upstream_status = 'deprecated'`（与导入页目录搜索的默认相反）：上游弃用说的是
-- 「厂商不再维护」，与我们自己的 `models.status='deprecated'`（已合并）是两套语义；
-- 选择器要能搜到灰测 / 弃用名，标由调用方按 `upstream_status` 自己打。
--
-- `%` / `_` / `\` 由调用方先转义（与作品搜索同一口径），这里只负责 `ESCAPE '\'`。
-- 条数由调用方给（`row_limit`）：公开端点是 20，写在 SQL 里会变成第二个出处。

-- name: SearchUpstreamModels :many
SELECT i.source,
       i.provider_id,
       i.provider_name,
       i.external_id,
       i.name,
       i.canonical_id,
       i.upstream_status,
       -- 空串 = 未收录。`models.slug` 有 CHECK 1..128，不会是空串，
       -- 所以这里用 COALESCE 而不是让 sqlc 把可空列摊成 string（NULL 扫进 string 会报错）。
       COALESCE(claim.model_slug, '') AS model_slug
FROM model_catalog_items i
LEFT JOIN LATERAL (
    SELECT m.slug AS model_slug
    FROM model_offerings r
    JOIN models m ON m.id = r.model_id
    WHERE r.authority = i.source
      AND r.provider_id = i.provider_id
      AND r.external_id = i.external_id
    LIMIT 1
) claim ON true
WHERE (sqlc.narg('q')::text IS NULL
       OR i.name ILIKE '%' || sqlc.narg('q')::text || '%' ESCAPE '\'
       OR i.external_id ILIKE '%' || sqlc.narg('q')::text || '%' ESCAPE '\'
       OR COALESCE(i.canonical_id, '') ILIKE '%' || sqlc.narg('q')::text || '%' ESCAPE '\'
       OR i.provider_name ILIKE '%' || sqlc.narg('q')::text || '%' ESCAPE '\')
ORDER BY i.name, i.provider_name, i.id
LIMIT sqlc.arg(row_limit)::int;
