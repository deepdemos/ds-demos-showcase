-- 0018: 分类词表落地（从线上站 deepdemos.top 的真实标签归并而来）。
--
-- # 数据来源
--
-- `GET https://deepdemos.top/api/v1/tags/tag-keys`（2026-11-06 实测，10 键 / 356 值）。
-- 旧站一张 tags 表混装了四类东西：内容分类、生成环境（platform）、模型、耗时。
-- 这里只落**内容分类**那三个键，其余各有归宿：
--
--   model       → models 表（不属于分类；模型是版本级声明，见 60-model.sql）
--   platform    → platforms / demo_revision_platforms
--   time/rounds → demo_revisions.gen_minutes / gen_rounds
--   plugin / skills / preset → 各自的实体表
--
-- # 为什么 slug 用中文原文
--
-- 旧站的 tag value 本来就是中文（「魔方」「3D建模」）。翻译成拼音或英文会给迁移
-- 引入第二份映射表，而这份映射没有任何人维护——下次对账时「mofang 到底是魔方
-- 还是魔方模拟器」就成了新问题。`UNIQUE (dimension, slug)` 没有字符集限制，
-- URL 里由调用方 encode。英文类型词（game / FPS / art）保持英文，它们本来就是英文。
--
-- # genre 的父子关系
--
-- `docs/标签域数据结构设计.md` §3.1 明写：数据里真实存在 `game → FPS`、
-- `game → 肉鸽`、`game → 跑酷` 的父子关系。所以旧 `type` 的 13 个值是顶层，
-- 旧 `game` 里的 18 个类型词是 `game` 的子项。`ck_classifications_shape`
-- 允许 genre 带 parent_id 且禁止带 aliases，正好对上。
--
-- # 被剔掉的值
--
--   demo(204) / html(43) / svg(25) / slides(0)——前三个是「这是个 demo」
--     「用了 html/svg」这类不构成分类的标记（demo 覆盖率 80.5%，等于没说）；
--     slides 引用为 0。它们的信息仍由 demo_types 与作品本身承载。
--   ppt——引用 0。
--   4 条 GBK 乱码脏数据（超级马里奥与魔方的乱码、重复的「地质正演」）。
--
-- # 幂等
--
-- 全部 ON CONFLICT DO NOTHING / DO UPDATE：重跑不会产生重复行，也不会覆盖
-- 管理员后来改过的 label（DO NOTHING 的就是分类行本身）。
-- ── 维度元数据（旧 tag_keys 的 label / mode / tier 在这里落地）──
INSERT INTO classification_dimensions (dimension, label, description, mode, tier, sort) VALUES
    ('genre',     '形态', '这件作品是什么东西：游戏、模拟、视觉、工具……', 'fixed', 1, 0),
    ('technique', '技术', '它主要靠什么技术做出来的', 'fixed', 2, 1),
    ('subject',   '题材', '它对着什么做的：某个具体作品、某个物件、某段历史', 'open', 2, 2)
ON CONFLICT (dimension) DO UPDATE
SET label = EXCLUDED.label, description = EXCLUDED.description,
    mode = EXCLUDED.mode, tier = EXCLUDED.tier, sort = EXCLUDED.sort;

-- ── genre 顶层 13 条（来自旧 type 的闭集；按旧站引用量排序）──
INSERT INTO classifications (dimension, slug, label, description, sort) VALUES ('genre', 'game', '小游戏', '小游戏类', 0) ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, sort) VALUES ('genre', 'simulation', '模拟', 'simulation', 1) ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, sort) VALUES ('genre', 'spatial', '空间', 'spatial', 2) ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, sort) VALUES ('genre', 'art', '艺术', 'art', 3) ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, sort) VALUES ('genre', 'music', '音乐', 'music', 4) ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, sort) VALUES ('genre', 'effect', '视觉特效', '视觉特效类', 5) ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, sort) VALUES ('genre', 'visualization', '可视化', 'visualization', 6) ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, sort) VALUES ('genre', 'education', '教育', 'education', 7) ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, sort) VALUES ('genre', 'action', '动作', 'action', 8) ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, sort) VALUES ('genre', 'widget', '小组件', '小组件类', 9) ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, sort) VALUES ('genre', 'puzzle', '解谜', 'puzzle', 10) ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, sort) VALUES ('genre', 'story', '叙事', 'story', 11) ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, sort) VALUES ('genre', 'utility', '工具', 'utility', 12) ON CONFLICT (dimension, slug) DO NOTHING;

-- ── genre 子级 18 条（父级 game，见 §3.1「game → FPS/肉鸽/跑酷」）──
INSERT INTO classifications (dimension, slug, label, description, parent_id, sort) SELECT 'genre', 'FPS', 'FPS', '', id, 0 FROM classifications WHERE dimension = 'genre' AND slug = 'game' ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, parent_id, sort) SELECT 'genre', 'FPV', 'FPV', '', id, 1 FROM classifications WHERE dimension = 'genre' AND slug = 'game' ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, parent_id, sort) SELECT 'genre', '肉鸽', '肉鸽', '', id, 2 FROM classifications WHERE dimension = 'genre' AND slug = 'game' ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, parent_id, sort) SELECT 'genre', '赛车', '赛车', '', id, 3 FROM classifications WHERE dimension = 'genre' AND slug = 'game' ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, parent_id, sort) SELECT 'genre', '动作肉鸽', '动作肉鸽', '', id, 4 FROM classifications WHERE dimension = 'genre' AND slug = 'game' ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, parent_id, sort) SELECT 'genre', '弹幕', '弹幕', '', id, 5 FROM classifications WHERE dimension = 'genre' AND slug = 'game' ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, parent_id, sort) SELECT 'genre', '跑酷', '跑酷', '', id, 6 FROM classifications WHERE dimension = 'genre' AND slug = 'game' ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, parent_id, sort) SELECT 'genre', '多边形射击', '多边形射击', '暴爽', id, 7 FROM classifications WHERE dimension = 'genre' AND slug = 'game' ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, parent_id, sort) SELECT 'genre', '靶场射击', '靶场射击', '', id, 8 FROM classifications WHERE dimension = 'genre' AND slug = 'game' ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, parent_id, sort) SELECT 'genre', '割草', '割草', '', id, 9 FROM classifications WHERE dimension = 'genre' AND slug = 'game' ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, parent_id, sort) SELECT 'genre', '增量', '增量', '', id, 10 FROM classifications WHERE dimension = 'genre' AND slug = 'game' ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, parent_id, sort) SELECT 'genre', '小球战争', '小球战争', '', id, 11 FROM classifications WHERE dimension = 'genre' AND slug = 'game' ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, parent_id, sort) SELECT 'genre', '平台跳跃', '平台跳跃', '', id, 12 FROM classifications WHERE dimension = 'genre' AND slug = 'game' ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, parent_id, sort) SELECT 'genre', '恐怖', '恐怖', '', id, 13 FROM classifications WHERE dimension = 'genre' AND slug = 'game' ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, parent_id, sort) SELECT 'genre', '战略模拟', '战略模拟', '', id, 14 FROM classifications WHERE dimension = 'genre' AND slug = 'game' ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, parent_id, sort) SELECT 'genre', '移动适配', '移动适配', '', id, 15 FROM classifications WHERE dimension = 'genre' AND slug = 'game' ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, parent_id, sort) SELECT 'genre', '解密', '解密', '', id, 16 FROM classifications WHERE dimension = 'genre' AND slug = 'game' ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, parent_id, sort) SELECT 'genre', '载具射击', '载具射击', '', id, 17 FROM classifications WHERE dimension = 'genre' AND slug = 'game' ON CONFLICT (dimension, slug) DO NOTHING;

-- ── technique 5 条（纯闭集：无父子、无别名，见 ck_classifications_shape）──
INSERT INTO classifications (dimension, slug, label, description, sort) VALUES ('technique', '3D建模', '3D建模', '', 0) ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, sort) VALUES ('technique', '仿真', '仿真', '', 1) ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, sort) VALUES ('technique', '动画', '动画', '', 2) ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, sort) VALUES ('technique', '图形学', '图形学', '', 3) ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, sort) VALUES ('technique', '机器人', '机器人', '', 4) ON CONFLICT (dimension, slug) DO NOTHING;

-- ── subject 56 条（开放集：允许别名、无父子）──
INSERT INTO classifications (dimension, slug, label, description, aliases, sort) VALUES ('subject', '魔方', '魔方', '', ARRAY['魔方模拟器', 'Rubik''s Cube', '三阶魔方'], 0) ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, aliases, sort) VALUES ('subject', '我的世界', '我的世界', '', ARRAY['MC', 'Minecraft'], 1) ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, aliases, sort) VALUES ('subject', '雨夜便利店', '雨夜便利店', '日式便利店街角微缩场景', '{}', 2) ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, aliases, sort) VALUES ('subject', '后室', '后室', '', '{}', 3) ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, aliases, sort) VALUES ('subject', '2048', '2048', '', '{}', 4) ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, aliases, sort) VALUES ('subject', '滚动的天空', '滚动的天空', '一款移动端音游', '{}', 5) ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, aliases, sort) VALUES ('subject', '兵棋推演', '兵棋推演', '', '{}', 6) ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, aliases, sort) VALUES ('subject', '马里奥', '马里奥', '', '{}', 7) ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, aliases, sort) VALUES ('subject', 'CS:GO', 'CS:GO', '', '{}', 8) ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, aliases, sort) VALUES ('subject', '三维弹球', '三维弹球', '', '{}', 9) ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, aliases, sort) VALUES ('subject', '明日方舟', '明日方舟', '', '{}', 10) ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, aliases, sort) VALUES ('subject', '星露谷物语', '星露谷物语', '', '{}', 11) ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, aliases, sort) VALUES ('subject', '重力迷宫', '重力迷宫', '', '{}', 12) ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, aliases, sort) VALUES ('subject', 'princess-rescue', 'princess-rescue', '', '{}', 13) ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, aliases, sort) VALUES ('subject', '五子棋', '五子棋', '', '{}', 14) ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, aliases, sort) VALUES ('subject', '元气骑士', '元气骑士', '', '{}', 15) ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, aliases, sort) VALUES ('subject', '原神', '原神', '', '{}', 16) ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, aliases, sort) VALUES ('subject', '地质', '地质', '正演', '{}', 17) ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, aliases, sort) VALUES ('subject', '孢子', '孢子', '', '{}', 18) ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, aliases, sort) VALUES ('subject', '战地风云', '战地风云', '', '{}', 19) ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, aliases, sort) VALUES ('subject', '植物大战僵尸', '植物大战僵尸', '', '{}', 20) ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, aliases, sort) VALUES ('subject', '池核', '池核', '', '{}', 21) ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, aliases, sort) VALUES ('subject', '泰拉瑞亚', '泰拉瑞亚', '', '{}', 22) ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, aliases, sort) VALUES ('subject', '红楼梦', '红楼梦', '红楼梦大观园·潇湘馆', '{}', 23) ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, aliases, sort) VALUES ('subject', '3D导演台', '3D导演台', '', '{}', 24) ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, aliases, sort) VALUES ('subject', 'SAO', 'SAO', '', '{}', 25) ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, aliases, sort) VALUES ('subject', 'psyche-dive', 'psyche-dive', '深潜 Psyche Dive', '{}', 26) ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, aliases, sort) VALUES ('subject', 'rimworld', 'rimworld', '环世界/边缘世界', '{}', 27) ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, aliases, sort) VALUES ('subject', 'teardown', 'teardown', 'teardown_like', '{}', 28) ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, aliases, sort) VALUES ('subject', 'temple-run', 'temple-run', '神庙逃亡类无尽跑酷', '{}', 29) ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, aliases, sort) VALUES ('subject', 'zuma', 'zuma', '祖玛', '{}', 30) ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, aliases, sort) VALUES ('subject', '人格胚养成计划', '人格胚养成计划', 'MBTI 认知功能养成', '{}', 31) ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, aliases, sort) VALUES ('subject', '历史', '历史', '', '{}', 32) ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, aliases, sort) VALUES ('subject', '双截龙', '双截龙', '', '{}', 33) ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, aliases, sort) VALUES ('subject', '咒术回战', '咒术回战', '', '{}', 34) ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, aliases, sort) VALUES ('subject', '坦克大战', '坦克大战', '', '{}', 35) ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, aliases, sort) VALUES ('subject', '宝可梦', '宝可梦', '', '{}', 36) ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, aliases, sort) VALUES ('subject', '幽灵诡计', '幽灵诡计', '', '{}', 37) ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, aliases, sort) VALUES ('subject', '拳皇', '拳皇', '', '{}', 38) ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, aliases, sort) VALUES ('subject', '捣蛋猪', '捣蛋猪', '', '{}', 39) ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, aliases, sort) VALUES ('subject', '方舟', '方舟', '方舟', '{}', 40) ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, aliases, sort) VALUES ('subject', '无限地牢', '无限地牢', '', '{}', 41) ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, aliases, sort) VALUES ('subject', '森之物语', '森之物语', '', '{}', 42) ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, aliases, sort) VALUES ('subject', '水果忍者', '水果忍者', '', '{}', 43) ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, aliases, sort) VALUES ('subject', '深岩银河', '深岩银河', '', '{}', 44) ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, aliases, sort) VALUES ('subject', '炸飞机', '炸飞机', '', '{}', 45) ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, aliases, sort) VALUES ('subject', '电车难题', '电车难题', '', '{}', 46) ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, aliases, sort) VALUES ('subject', '空洞骑士', '空洞骑士', '', '{}', 47) ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, aliases, sort) VALUES ('subject', '糖豆人', '糖豆人', '', '{}', 48) ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, aliases, sort) VALUES ('subject', '红警', '红警', '', '{}', 49) ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, aliases, sort) VALUES ('subject', '街角咖啡馆', '街角咖啡馆', '', '{}', 50) ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, aliases, sort) VALUES ('subject', '西班牙内战', '西班牙内战', '', '{}', 51) ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, aliases, sort) VALUES ('subject', '超级马里奥', '超级马里奥', '', '{}', 52) ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, aliases, sort) VALUES ('subject', '铁巢重炮', '铁巢重炮', '', '{}', 53) ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, aliases, sort) VALUES ('subject', '骨牌', '骨牌', '', '{}', 54) ON CONFLICT (dimension, slug) DO NOTHING;
INSERT INTO classifications (dimension, slug, label, description, aliases, sort) VALUES ('subject', '鸭科夫', '鸭科夫', '', '{}', 55) ON CONFLICT (dimension, slug) DO NOTHING;
