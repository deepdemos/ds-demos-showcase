-- 身份域：会话查询。这一刀只有「我是谁」，建账号与会话在下一刀。

-- name: GetMeBySession :one
-- 会话有效性一次判完：没过期、没被吊销、账号还在且没注销。其余 status 一律放行——
-- 被封/待验证的账号也要能读出自己；但 deleted 例外，会话必须失效，不能靠注销流程兜底。
SELECT a.sub,
       p.username,
       p.display_name,
       e.email,
       a.role,
       a.status,
       a.level,
       a.created_at
FROM web_sessions s
JOIN accounts a ON a.sub = s.account_sub
JOIN profiles p ON p.account_id = a.id
JOIN account_emails e ON e.account_id = a.id AND e.is_primary
WHERE s.id = $1
  AND s.revoked_at IS NULL
  AND s.expires_at > now()
  AND a.status <> 'deleted';
