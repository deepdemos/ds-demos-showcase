-- 0015_demo_views.sql —— 浏览计数的去重表
--
-- 为什么需要一张表：`demo_stats.view_count` 只有计数列，回答不了「这个人今天看过没有」。
-- 需求 DEMO-041 要求「刷新页面不把浏览数刷成多次」，所以必须有个地方记住「谁 · 哪个作品 · 哪一天」。
--
-- 为什么键是「人 + 作品 + 日期」而不是「人 + 作品」：
-- 一个人隔三差五回来看是真实兴趣，不该被永远压成一次；而同一天里刷新十遍只算一次。
-- 用日期而不是时间戳差，是因为「今天」这个边界可解释——运营看得懂，也能按天清理。
--
-- 为什么不存 IP：`viewer_key` 存的是哈希后的标识（登录用户是 `user:<id>`，
-- 匿名是 `anon:` + HMAC(ip)），与 `demo_ratings.rater_key` 同一套口径。
-- 明文 IP 入不了库，也就不存在「日志/备份里泄露访问者」这条路径。
--
-- 清理：这张表只增不减，按天分区或定期删旧行都行，本轮不做（已知松点）。

CREATE TABLE demo_views (
    demo_id     bigint      NOT NULL REFERENCES demos (id) ON DELETE CASCADE,
    viewer_key  text        NOT NULL,
    viewed_on   date        NOT NULL DEFAULT current_date,
    created_at  timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (demo_id, viewer_key, viewed_on),
    CONSTRAINT ck_demo_views_key CHECK (length(viewer_key) BETWEEN 1 AND 128)
);

COMMENT ON TABLE demo_views IS
    '浏览去重：同一个人（或其匿名哈希）对同一作品在同一个自然日内只算一次。只用于去重，不用于分析。';
COMMENT ON COLUMN demo_views.viewer_key IS
    '登录用户 = user:<accounts.id>；匿名 = anon: + HMAC(ip)。与 demo_ratings.rater_key 同一套口径。';

-- 按天清理与「某作品最近热度」都要走 viewed_on，所以给它一条独立索引。
-- 主键最左是 demo_id，按日期扫全表就用不上了。
CREATE INDEX ix_demo_views_viewed_on ON demo_views (viewed_on);
