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

-- ===== 登录 / 登出 =====

-- name: GetLoginByEmail :one
-- 登录标识是邮箱或用户名，先按邮箱找，找不到再按用户名——两条分开查是为了避免
-- 「某人的用户名恰好等于另一个人的邮箱」时命中错的那个人。
SELECT a.id, a.sub, a.status, a.role, a.level, a.created_at,
       p.username, p.display_name, e.email, c.secret_hash
FROM accounts a
JOIN profiles p ON p.account_id = a.id
JOIN account_emails e ON e.account_id = a.id AND e.is_primary
JOIN credentials c ON c.account_id = a.id AND c.kind = 'password'
WHERE lower(e.email) = lower($1);

-- name: GetLoginByUsername :one
SELECT a.id, a.sub, a.status, a.role, a.level, a.created_at,
       p.username, p.display_name, e.email, c.secret_hash
FROM accounts a
JOIN profiles p ON p.account_id = a.id
JOIN account_emails e ON e.account_id = a.id AND e.is_primary
JOIN credentials c ON c.account_id = a.id AND c.kind = 'password'
WHERE lower(p.username) = lower($1);

-- name: RevokeWebSession :exec
UPDATE web_sessions SET revoked_at = now() WHERE id = $1 AND revoked_at IS NULL;

-- ===== 登录限流（身份域 §11.3）=====

-- name: CountLoginFailures :one
-- 一次查询算完四层口径。account_id 传 NULL 时账号维度恒为 0（`account_id = NULL` 不成立），
-- IP 维度照常计数——这正是「无效邮箱也要被 IP 层看到」的实现方式。
--
-- 账号维度的「成功登录清零」靠 `created_at > 最后一次成功` 表达，而不是删行：
-- 行是不可变的证据，删了就说不清「这个 IP 之前试过什么」。
WITH last_ok AS (
    SELECT COALESCE(max(created_at), '-infinity'::timestamptz) AS at
    FROM login_attempts
    WHERE account_id = sqlc.narg(account_id)::bigint AND succeeded
)
SELECT
    count(*) FILTER (
        WHERE account_id = sqlc.narg(account_id)::bigint
          AND ip_hash = sqlc.arg(ip_hash)::text
    )::int AS account_ip_fails,
    count(*) FILTER (
        WHERE account_id = sqlc.narg(account_id)::bigint
          AND created_at > last_ok.at
    )::int AS account_fails,
    count(*) FILTER (WHERE ip_hash = sqlc.arg(ip_hash)::text)::int AS ip_fails,
    count(*) FILTER (
        WHERE ip_hash = sqlc.arg(ip_hash)::text
          AND created_at > now() - interval '1 hour'
    )::int AS ip_fails_hour
FROM login_attempts, last_ok
WHERE succeeded = false
  AND created_at > now() - interval '10 minutes';

-- name: RecordLoginAttempt :exec
-- identifier_hash 是「用户输进来的那个标识符」的 HMAC，与账号存不存在无关。
-- 第②层按它计数，所以对着不存在的邮箱撞库同样会被拦住。
INSERT INTO login_attempts (ip_hash, account_id, identifier_hash, succeeded)
VALUES (sqlc.arg(ip_hash), sqlc.narg(account_id), sqlc.narg(identifier_hash), sqlc.arg(succeeded));

-- name: CountIdentifierFailures :one
-- 第②层（身份域 §11.3）：同一标识符 10 分钟内失败次数，成功登录后清零。
-- 不区分账号是否存在——这正是「无效邮箱也要被要求验证码」的实现。
WITH last_ok AS (
    SELECT COALESCE(max(created_at), '-infinity'::timestamptz) AS at
    FROM login_attempts
    WHERE identifier_hash = sqlc.arg(identifier_hash)::text AND succeeded
)
SELECT count(*) FILTER (WHERE created_at > last_ok.at)::int AS fails
FROM login_attempts, last_ok
WHERE succeeded = false
  AND identifier_hash = sqlc.arg(identifier_hash)::text
  AND created_at > now() - interval '10 minutes';

-- name: PurgeLoginAttempts :exec
DELETE FROM login_attempts WHERE created_at < now() - interval '1 hour';
