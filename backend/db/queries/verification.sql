-- kernel/verification：通用验证码的存取。不认识「登录」或「改邮箱」，只认 purpose。

-- name: CreateVerificationCode :one
-- 过期时刻用数据库的 now() 算，不用 Go 的时间：判定也走 SQL 的 now()，
-- 两边同一个时钟来源，不会因为应用机与库时钟差不一致而提前/延后失效。
INSERT INTO verification_codes (purpose, identifier_hash, code_hash, account_id, expires_at)
VALUES ($1, $2, $3, $4, now() + make_interval(secs => sqlc.arg(ttl_seconds)::int))
RETURNING id;

-- name: LatestVerificationCode :one
-- 只取「未消费且未过期」里最新的那一条。旧码在新码发出后自然作废（消费时按 id 匹配）。
SELECT id, code_hash, attempts
FROM verification_codes
WHERE purpose = $1
  AND identifier_hash = $2
  AND consumed_at IS NULL
  AND expires_at > now()
ORDER BY id DESC
LIMIT 1;

-- name: ConsumeVerificationCode :execrows
-- 抢占式消费：只有把 consumed_at 从 NULL 改成功的那个请求算数，
-- 两个并发请求同时拿着正确码也只可能有一个成功。
UPDATE verification_codes
SET consumed_at = now()
WHERE id = $1 AND consumed_at IS NULL;

-- name: InvalidateVerificationCode :exec
UPDATE verification_codes
SET consumed_at = now()
WHERE id = $1 AND consumed_at IS NULL;

-- name: BumpVerificationAttempts :one
UPDATE verification_codes
SET attempts = attempts + 1
WHERE id = $1
RETURNING attempts;

-- name: CountRecentVerificationCodes :one
SELECT count(*) FILTER (WHERE created_at > now() - interval '1 minute')::int AS last_minute,
       count(*) FILTER (WHERE created_at > now() - interval '1 hour')::int   AS last_hour
FROM verification_codes
WHERE purpose = $1 AND identifier_hash = $2;

-- name: PurgeVerificationCodes :exec
DELETE FROM verification_codes WHERE created_at < now() - interval '1 day';
