-- 0011：新增权限 `demo.publish`（把作品从 draft 推到 published + 落渠道行）。
--
-- 为什么不复用 `demo.curate`：`curate` 管的是「这条内容能不能出现在某个渠道、什么语言」，
-- 是**上架后的可见面**；`publish` 管的是「这个作品要不要对外存在」，是**生命周期跃迁**。
-- 两者现在都由同一个人（admin）持有，但混成一个键以后想拆就得改数据。
--
-- 治理域 §4.3 的权限清单同步 21 → 22。

INSERT INTO permissions (key, description) VALUES
    ('demo.publish', '发布（draft → published + 落渠道行）')
ON CONFLICT (key) DO NOTHING;

-- 基线里 `SELECT 'admin', key FROM permissions` 已经把当时的 21 个绑给 admin，
-- 后加的权限要显式补绑一次。
INSERT INTO role_permissions (role_key, permission_key) VALUES
    ('admin', 'demo.publish')
ON CONFLICT DO NOTHING;
