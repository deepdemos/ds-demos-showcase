-- 通知域（#437 / NTF-001 – NTF-008）。

-- name: GetDemoNotifyTarget :one
-- 通知接收者：作品作者对应的账号。作者注销后 account 为空，调用方跳过。
SELECT d.slug,
       COALESCE(d.title, '')::text AS title,
       ac.id AS account_id
FROM demos d
JOIN users u ON u.id = d.author_id
LEFT JOIN accounts ac ON ac.sub = u.sub
WHERE d.slug = @slug;

-- name: InsertNotification :exec
-- payload 是事件自己的数据（reason / score），由调用方序列化。
--
-- 对象用「类型 + 键 + 快照标题」写，不存外键（NTF-007）。demo_slug / demo_title 是
-- 回滚窗口里的过渡双写：只有作品类事件填它们，非作品对象给 NULL / 空串；读路径不读这两列。
INSERT INTO notifications (
    account_id, kind, object_type, object_key, object_title, demo_slug, demo_title, payload
)
VALUES (
    @account_id, @kind, @object_type, @object_key, @object_title,
    sqlc.narg('demo_slug')::text, @demo_title, @payload::jsonb
);

-- name: ListNotifications :many
-- accessible：对象当前是否可访问。判据**按对象类型分支**：
--   · 作品（demo）：只有在公开视图 demo_public_demos 里才算可访问（软删 / 下架 / 擦除后 false）。
--   · 非作品（model / proposal）：**不套**作品可见性——它们的读取入口由各自域决定，
--     当前过渡口径是「对象键在就算可访问」；各域的判据落地时在这里加分支。
-- 旧列 demo_slug / demo_title 一律不读（它们只剩回滚窗口里的双写意义，见迁移 0041）。
SELECT n.id,
       n.kind,
       n.object_type,
       n.object_key,
       n.object_title,
       n.payload,
       n.read_at,
       n.created_at,
       (CASE
           WHEN n.object_type = 'demo' THEN EXISTS (
               SELECT 1 FROM demos d
               JOIN demo_public_demos p ON p.demo_id = d.id
               WHERE d.slug = n.object_key
           )
           ELSE n.object_key <> ''
       END)::boolean AS accessible
FROM notifications n
WHERE n.account_id = @account_id
ORDER BY n.created_at DESC, n.id DESC
LIMIT @row_limit OFFSET @row_offset;

-- name: CountNotifications :one
SELECT count(*)::bigint AS total
FROM notifications
WHERE account_id = @account_id;

-- name: CountUnreadNotifications :one
SELECT count(*)::bigint AS total
FROM notifications
WHERE account_id = @account_id AND read_at IS NULL;

-- name: MarkNotificationRead :execrows
-- 只认自己的；已经读过也是 0 行，调用方照样 204（幂等）。
UPDATE notifications
SET read_at = now()
WHERE id = @id AND account_id = @account_id AND read_at IS NULL;

-- name: MarkAllNotificationsRead :execrows
UPDATE notifications
SET read_at = now()
WHERE account_id = @account_id AND read_at IS NULL;

-- name: DeleteExpiredNotifications :execrows
-- 读时清理：保留天数来自站点设置，缺键时调用方兜 90。
DELETE FROM notifications
WHERE account_id = @account_id
  AND created_at < now() - make_interval(days => @days::int);


-- name: GetNotificationReadAt :one
-- 单项已读 0 行时区分「已经读过」与「不是自己的 / 不存在」：前者 204，后者 404。
SELECT read_at
FROM notifications
WHERE id = @id AND account_id = @account_id;
