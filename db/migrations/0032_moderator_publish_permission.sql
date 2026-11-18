-- 0032：把 `demo.publish` 绑给 moderator（Discussion #464 d1 B）。
--
-- 2026-11-14 用户拍板：审核员通过后自己发布，不再等管理员。权限键本身由 0011 建好，
-- 这里只加一条角色绑定；`permissions` 清单不变（仍是 23 个）。
--
-- 审计不新增字段：发布事件本来就写 `audit_log.actor_role`（domain/demo/publish.go），
-- moderator 发布时那列自然是 `moderator`，可追溯。
--
-- 治理域 §4.3 的 `moderator` 一列同步为 ✅，理由写在那节的注里。

INSERT INTO role_permissions (role_key, permission_key) VALUES
    ('moderator', 'demo.publish')
ON CONFLICT DO NOTHING;
