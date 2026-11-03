-- 授权查询（治理域 §4「RBAC」）。
--
-- 为什么单独一个文件：权限判定将来不只被预览用（审核、发布、删除都要），
-- 放在 demos.sql 里会让别的域看起来像在借用 Demo 域的查询。

-- name: HasPermission :one
-- 角色是 `accounts.role` 单列（没有 account_roles 关联表），权限经 role_permissions 展开。
-- 账号不存在、角色没有该权限、权限键拼错——三种情况一律返回 false，不区分：
-- 区分了就是把「这个权限键存不存在」暴露给调用方。
SELECT EXISTS (
    SELECT 1
    FROM accounts a
    JOIN role_permissions rp ON rp.role_key = a.role
    WHERE a.sub = $1 AND rp.permission_key = $2
)::bool AS allowed;
