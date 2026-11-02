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

-- ===== 注册（一事务五张表）=====

-- name: CreateAccount :one
INSERT INTO accounts (sub, status, role)
VALUES ($1, 'active', 'user')
RETURNING id, created_at;

-- name: CreateProfile :exec
INSERT INTO profiles (account_id, username, display_name)
VALUES ($1, $2, $3);

-- name: CreatePrimaryEmail :exec
INSERT INTO account_emails (account_id, email, is_primary, verified_at)
VALUES ($1, $2, true, NULL);

-- name: CreatePasswordCredential :exec
INSERT INTO credentials (account_id, kind, identifier, secret_hash)
VALUES ($1, 'password', '', $2);

-- name: CreateWebSession :exec
-- 不写 expires_at：表上有 DEFAULT now() + interval '30 days'。绝对寿命只有那一个来源，
-- 在 Go 里再写一份就等于两个数字迟早对不上（运行手册 §3 记的是这条线的 anchor）。
INSERT INTO web_sessions (id, account_sub, username, display_name, role, status, verified_at)
VALUES ($1, $2, $3, $4, $5, $6, now());
