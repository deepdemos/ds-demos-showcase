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
-- 三层各自的窗口必须写在**各自的 FILTER 里**，不能写在外层 WHERE。
--
-- 旧写法外层 `created_at > now() - interval '10 minutes'` 会把内层的
-- 「1 小时」压成「10 分钟」，于是 `ip_fails_hour` 实际是「200 次/10 分钟」——
-- 不是设计要的「200 次/小时」，而且更严，会误伤正常用户。外层只能筛最宽的那个
-- 窗口（1 小时），每一档在 FILTER 里再收窄。
--
-- account_id 传 NULL 时账号维度恒为 0（`account_id = NULL` 不成立），
-- IP 维度照常计数——这正是「无效邮箱也要被 IP 层看到」的实现方式。
--
-- kind = 'login'：发验证码的记录同表但不算登录失败，不能混进来。
SELECT
    count(*) FILTER (
        WHERE account_id = sqlc.narg(account_id)::bigint
          AND ip_hash = sqlc.arg(ip_hash)::text
          AND created_at > now() - interval '10 minutes'
    )::int AS account_ip_fails,
    count(*) FILTER (
        WHERE ip_hash = sqlc.arg(ip_hash)::text
          AND created_at > now() - interval '10 minutes'
    )::int AS ip_fails,
    count(*) FILTER (
        WHERE ip_hash = sqlc.arg(ip_hash)::text
          AND created_at > now() - interval '1 hour'
    )::int AS ip_fails_hour
FROM login_attempts
WHERE succeeded = false
  AND kind = 'login'
  AND created_at > now() - interval '1 hour';

-- name: RecordLoginAttempt :exec
-- identifier_hash 是「用户输进来的那个标识符」的 HMAC，与账号存不存在无关。
-- 第②层按它计数，所以对着不存在的邮箱撞库同样会被拦住。
--
-- kind 把「登录失败」与「发了一封验证码」分开：两者同表，但前者算失败、后者不算。
-- 混在一起的话，正常用户连要两次码就会触发第①层的锁定。
INSERT INTO login_attempts (ip_hash, account_id, identifier_hash, succeeded, kind)
VALUES (sqlc.arg(ip_hash), sqlc.narg(account_id), sqlc.narg(identifier_hash), sqlc.arg(succeeded), sqlc.arg(kind));

-- name: CountEmailCodeSends :one
-- 发码配额：同一个 IP 一小时内发了多少封。
--
-- 它挡的是「换着邮箱把站点当邮件炮台」，不是挡某个人收不到信——后者由
-- kernel 里按 (purpose, identifier) 的 1/分 5/时 管。两层各管一件事。
SELECT count(*)::int AS sends
FROM login_attempts
WHERE kind = 'email_code'
  AND ip_hash = sqlc.arg(ip_hash)::text
  AND created_at > now() - interval '1 hour';

-- name: CountIdentifierFailures :one
-- 第②层（身份域 §11.3）：同一标识符 10 分钟内失败次数，成功登录后清零。
-- 不区分账号是否存在——这正是「无效邮箱也要被要求验证码」的实现。
-- last_ok 必须也带 kind = 'login'：发码记录同样是 succeeded=true，
-- 不筛的话「刚刚发过一封验证码」会被当成「刚刚登录成功」，把之前 5 次失败
-- 一起清零，第②层就永远触发不了。
WITH last_ok AS (
    SELECT COALESCE(max(created_at), '-infinity'::timestamptz) AS at
    FROM login_attempts
    WHERE identifier_hash = sqlc.arg(identifier_hash)::text
      AND succeeded
      AND kind = 'login'
)
SELECT count(*) FILTER (WHERE created_at > last_ok.at)::int AS fails
FROM login_attempts, last_ok
WHERE succeeded = false
  AND kind = 'login'
  AND identifier_hash = sqlc.arg(identifier_hash)::text
  AND created_at > now() - interval '10 minutes';

-- name: PurgeLoginAttempts :exec
DELETE FROM login_attempts WHERE created_at < now() - interval '1 hour';

-- ===== 密码重置 =====

-- name: GetPasswordResetTarget :one
-- 找回密码只认**邮箱**，不认用户名：用户名是公开的，拿它发起重置等于给攻击者
-- 一个「只要知道昵称就能骚扰到人」的入口。
SELECT a.id, a.sub, a.status, p.username, p.display_name, e.email
FROM accounts a
JOIN profiles p ON p.account_id = a.id
JOIN account_emails e ON e.account_id = a.id AND e.is_primary
JOIN credentials c ON c.account_id = a.id AND c.kind = 'password'
WHERE lower(e.email) = lower($1);

-- name: UpdatePasswordHash :one
-- 同时写 rotated_at：这两件事必须同一个事务，否则会出现「密码换了但旧会话还有效」
-- 或者「会话被踢了但密码没换成」的中间态。
--
-- 把 rotated_at 用 RETURNING 带回来，而不是更新后再 SELECT 一次：两句话之间
-- 别人也可能改这一行，读回来的就不一定是本次写入的那个时刻了。
UPDATE credentials
SET secret_hash = $2,
    rotated_at  = now(),
    last_used_at = now()
WHERE account_id = $1 AND kind = 'password'
RETURNING rotated_at;

-- name: RevokeSessionsBefore :execrows
-- 撤销这个账号在 $2 之前签发的全部会话。$2 是刚写下的 rotated_at，
-- 所以本次重置自己新发的会话（created_at 更晚）不受影响。
UPDATE web_sessions
SET revoked_at = now()
WHERE account_sub = $1
  AND revoked_at IS NULL
  AND created_at < $2;
