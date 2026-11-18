-- 账号浏览历史（#426 第二半）。
--
-- 写入口挂在 POST /demos/{slug}/view（浏览计数那条路）上；读/清空在 /me/history。
-- 上限与开关都读设置域：history.record（user 级，默认 true）、
-- history.max_entries_account（site 级，默认 200）。默认值只在 setting_keys 里，
-- 这里只在「连注册表都缺行」时兜一个 true/200——那属于数据事故，不该让浏览挂掉。

-- name: GetUserHistoryEnabled :one
-- 有没有 user_preferences 行都成立：缺行读注册表默认值，注册表缺行才兜 true。
SELECT COALESCE(
    (SELECT (up.value #>> '{}')::boolean
       FROM user_preferences up
      WHERE up.account_id = @account_id
        AND up.key = 'history.record'),
    (SELECT (k.default_value #>> '{}')::boolean
       FROM setting_keys k
      WHERE k.key = 'history.record'
        AND k.scope = 'user'),
    true
)::boolean AS enabled;

-- name: GetAccountHistoryLimit :one
SELECT COALESCE(
    (SELECT (s.value #>> '{}')::integer
       FROM site_settings s
      WHERE s.key = 'history.max_entries_account'),
    (SELECT (k.default_value #>> '{}')::integer
       FROM setting_keys k
      WHERE k.key = 'history.max_entries_account'
        AND k.scope = 'site'),
    200
)::integer AS max_entries;

-- name: SetUserHistoryEnabled :exec
-- 写 user_preferences：value_type 与 scope 必须与 setting_keys 的注册行一致（复合外键）。
INSERT INTO user_preferences (account_id, key, scope, value_type, value, updated_at)
VALUES (@account_id, 'history.record', 'user', 'boolean', to_jsonb(@enabled::boolean), now())
ON CONFLICT (account_id, key) DO UPDATE
SET value      = EXCLUDED.value,
    value_type = EXCLUDED.value_type,
    updated_at = now();

-- name: UpsertAccountViewHistory :exec
-- 一件作品一行：重复浏览刷新时刻。软删 / 擦除的作品不记——它们打不开，也不该占历史。
INSERT INTO account_view_history (account_id, demo_id, viewed_at)
SELECT @account_id, d.id, now()
FROM demos d
WHERE d.slug = @slug
  AND d.deleted_at IS NULL
  AND d.erased_at IS NULL
  AND (
      d.author_id = (
          SELECT u.id FROM users u
          JOIN accounts a ON a.sub = u.sub
          WHERE a.id = @account_id
      )
      OR (
          d.lifecycle_state = 'published'
          AND d.published_revision_id IS NOT NULL
          AND EXISTS (SELECT 1 FROM demo_visible_channels c WHERE c.demo_id = d.id)
      )
  )
ON CONFLICT (account_id, demo_id) DO UPDATE
SET viewed_at = EXCLUDED.viewed_at;

-- name: TrimAccountViewHistory :exec
-- 超过上限时只留最近 keep 条。用 NOT IN 而不是窗口函数：子查询只有一列、非空，
-- 语义比 row_number 那套短，也更容易在 psql 里直接跑。
DELETE FROM account_view_history AS outer_h
WHERE outer_h.account_id = @account_id
  AND outer_h.demo_id NOT IN (
      SELECT inner_h.demo_id
      FROM account_view_history AS inner_h
      WHERE inner_h.account_id = @account_id
      ORDER BY inner_h.viewed_at DESC, inner_h.demo_id DESC
      LIMIT @keep
  );

-- name: ListAccountHistory :many
-- 只列还能打开的作品：软删 / 擦除的行对外是 404，历史页不该给死链。
SELECT d.slug, d.title, d.cover_url, h.viewed_at
FROM account_view_history h
JOIN demos d ON d.id = h.demo_id
WHERE h.account_id = @account_id
  AND d.deleted_at IS NULL
  AND d.erased_at IS NULL
  AND (
      d.author_id = (
          SELECT u.id FROM users u
          JOIN accounts a ON a.sub = u.sub
          WHERE a.id = @account_id
      )
      OR (
          d.lifecycle_state = 'published'
          AND d.published_revision_id IS NOT NULL
          AND EXISTS (SELECT 1 FROM demo_visible_channels c WHERE c.demo_id = d.id)
      )
  )
ORDER BY h.viewed_at DESC, d.id DESC
LIMIT @row_limit OFFSET @row_offset;

-- name: CountAccountHistory :one
SELECT count(*)::bigint AS total
FROM account_view_history h
JOIN demos d ON d.id = h.demo_id
WHERE h.account_id = @account_id
  AND d.deleted_at IS NULL
  AND d.erased_at IS NULL
  AND (
      d.author_id = (
          SELECT u.id FROM users u
          JOIN accounts a ON a.sub = u.sub
          WHERE a.id = @account_id
      )
      OR (
          d.lifecycle_state = 'published'
          AND d.published_revision_id IS NOT NULL
          AND EXISTS (SELECT 1 FROM demo_visible_channels c WHERE c.demo_id = d.id)
      )
  );

-- name: ClearAccountHistory :execrows
DELETE FROM account_view_history WHERE account_id = @account_id;
