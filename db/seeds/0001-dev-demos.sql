-- 开发用种子数据：真实作品名（来自仓库外 assets/ 的十个可运行 demo）+ 刻意准备的边界行。
--
-- 为什么要有边界行：列表页与详情页最容易在这几处出错——空描述、超长标题、
-- 没有封面、以及三种墓碑态。只塞「漂亮数据」的话，页面在本地看起来完美，
-- 一上真实数据就露馅。
--
-- 全部按 slug 幂等（ON CONFLICT DO UPDATE），可以反复跑。
-- 不进迁移：种子数据属于开发环境，不该被 migrate up 带进任何真实库。

-- ===== 24 条已发布作品（10 个真实作品 + 14 条同风格补充，凑够分页） =====
INSERT INTO demos (slug, title, description, cover_url, moderation_state, lifecycle_state, created_at, updated_at)
VALUES
    ('blackhole-merge', '双黑洞合并', '基于真实物理的双黑洞并合模拟，引力波波形随手可调。', NULL, 'approved', 'published', '2026-08-18 20:05:00+08', '2026-08-18 20:05:00+08'),
    ('mc1', 'MC 方块世界', '一个可在浏览器里跑的小型体素世界，支持第一人称漫游。', NULL, 'approved', 'published', '2026-08-18 21:00:00+08', '2026-08-18 21:00:00+08'),
    ('mcbench', 'MC 跑分基准', '用统一的体素场景给不同设备的 WebGL 性能打分，给出 9 分制结论。', NULL, 'approved', 'published', '2026-08-18 22:10:00+08', '2026-08-18 22:10:00+08'),
    ('card-roguelike', '卡牌肉鸽', '牌组构建 + 随机事件，一局约五分钟的轻量 roguelike。', NULL, 'approved', 'published', '2026-08-19 09:30:00+08', '2026-08-19 09:30:00+08'),
    ('cloth-physics', '布料物理', '实时布料模拟，可以拖拽、剪开、叠加多层。', NULL, 'approved', 'published', '2026-08-19 10:15:00+08', '2026-08-19 10:15:00+08'),
    ('pixel-beads', '拼豆', '把图片转成拼豆图纸，支持导出带编号的排布表。', NULL, 'approved', 'published', '2026-08-19 11:00:00+08', '2026-08-19 11:00:00+08'),
    ('hourglass', '沙漏', '颗粒级沙子模拟，摇晃会改变下落形态。', NULL, 'approved', 'published', '2026-08-19 13:20:00+08', '2026-08-19 13:20:00+08'),
    ('rolling-sky', '滚动的天空', '音乐节奏与跑酷结合，随节拍切换地形。', NULL, 'approved', 'published', '2026-08-19 14:40:00+08', '2026-08-19 14:40:00+08'),
    ('gravity-maze', '重力迷宫', '转动整个世界而不是转球，靠重力把小球送进终点。', NULL, 'approved', 'published', '2026-08-19 16:05:00+08', '2026-08-19 16:05:00+08'),
    ('rubiks-cube', '魔方', '可交互的三阶魔方，支持随机打乱与逐步还原提示。', NULL, 'approved', 'published', '2026-08-19 17:30:00+08', '2026-08-19 17:30:00+08'),
    ('fluid-ink', '流体墨迹', '基于速度场的墨迹扩散，笔刷压感会改变黏度。', NULL, 'approved', 'published', '2026-08-20 09:00:00+08', '2026-08-20 09:00:00+08'),
    ('lorenz-attractor', '洛伦兹吸引子', '三维粒子沿洛伦兹方程流动，初值差一点结果完全不同。', NULL, 'approved', 'published', '2026-08-20 10:20:00+08', '2026-08-20 10:20:00+08'),
    ('wave-interference', '波的干涉', '双缝干涉与多源叠加，实时调频率与相位。', NULL, 'approved', 'published', '2026-08-20 11:40:00+08', '2026-08-20 11:40:00+08'),
    ('game-of-life', '生命游戏', '康威生命游戏，可画初态、可回放，附常见图样库。', NULL, 'approved', 'published', '2026-08-20 13:00:00+08', '2026-08-20 13:00:00+08'),
    ('boids-flock', '鸟群算法', 'Boids 三条规则生成群体行为，可调分离与聚合权重。', NULL, 'approved', 'published', '2026-08-20 14:10:00+08', '2026-08-20 14:10:00+08'),
    ('marching-squares', '等值面地形', 'Marching Squares 从噪声生成可走地形，带洞穴。', NULL, 'approved', 'published', '2026-08-20 15:25:00+08', '2026-08-20 15:25:00+08'),
    ('truchet-tiles', '特吕谢图案', '一行代码画不出来的特吕谢瓷砖，支持多层叠加。', NULL, 'approved', 'published', '2026-08-20 16:45:00+08', '2026-08-20 16:45:00+08'),
    ('voronoi-shatter', '泰森多边形碎裂', '点击让平面按 Voronoi 图碎裂，碎块有独立物理。', NULL, 'approved', 'published', '2026-08-21 09:15:00+08', '2026-08-21 09:15:00+08'),
    ('rope-bridge', '绳索桥', 'Verlet 积分做的绳索，可承重、可断。', NULL, 'approved', 'published', '2026-08-21 10:35:00+08', '2026-08-21 10:35:00+08'),
    ('sph-water', 'SPH 水体', '光滑粒子流体模拟，支持倾倒与容器碰撞。', NULL, 'approved', 'published', '2026-08-21 11:55:00+08', '2026-08-21 11:55:00+08'),
    ('path-tracer', '路径追踪器', '浏览器里的简易路径追踪，带景深与软阴影。', NULL, 'approved', 'published', '2026-08-21 13:30:00+08', '2026-08-21 13:30:00+08'),
    ('raymarch-sdf', 'SDF 光线步进', '用有符号距离场雕刻形体，改一行参数就换一个世界。', NULL, 'approved', 'published', '2026-08-21 14:50:00+08', '2026-08-21 14:50:00+08'),
    ('shader-gallery', '着色器画廊', '二十个片元着色器小样，按噪声类型分类。', NULL, 'approved', 'published', '2026-08-21 16:10:00+08', '2026-08-21 16:10:00+08'),
    ('piano-roll', '钢琴卷帘', '网页版钢琴卷帘，支持 MIDI 导入与简单合成。', NULL, 'approved', 'published', '2026-08-21 17:40:00+08', '2026-08-21 17:40:00+08'),

-- ===== 边界行：这 6 条是给页面挑错的，不是给用户看的 =====

    -- 空描述：列表页与详情页都必须渲染成「暂无简介」，不能出现空白块或 null
    ('edge-no-description', '没有简介的作品', NULL, NULL, 'approved', 'published', '2026-08-22 09:00:00+08', '2026-08-22 09:00:00+08'),

    -- 超长标题：130 个字符（上限 200）。用来验证列表卡片不会被撑破
    ('edge-long-title', '这是一个用来测试超长标题换行与截断行为的作品它的标题刻意写得很长很长一直长到接近数据库允许的两百个字符上界好让我们看看列表卡片在这种情况下会变成什么样子会不会把布局撑破或者把旁边的元素挤出去以及在窄屏下是折行还是横向溢出这些都需要真实渲染一遍才知道所以就这么长', '标题 130 个字符，用于验证换行与截断。', NULL, 'approved', 'published', '2026-08-22 09:30:00+08', '2026-08-22 09:30:00+08'),

    -- 已归档：列表里不出现，详情页必须是 200 墓碑（标题与简介为 null）
    ('edge-archived', '已归档的作品', '作者自己下架了它。', NULL, 'approved', 'archived', '2026-08-22 10:00:00+08', '2026-08-22 10:00:00+08'),

    -- 被移除：列表里不出现，详情页同样是 200 墓碑（与归档内容必须不同，见标准对象 §ST-2）
    ('edge-removed', '被移除的作品', '因违规被管理员移除。', NULL, 'approved', 'removed', '2026-08-22 10:30:00+08', '2026-08-22 10:30:00+08'),

    -- 软删：deleted_at 有值而生命周期仍是 published。列表不出现，详情页是 200 墓碑
    ('edge-soft-deleted', '被作者删除的作品', '删除后进入回收站，未擦除。', NULL, 'approved', 'published', '2026-08-22 11:00:00+08', '2026-08-22 11:00:00+08'),

    -- 已擦除：逻辑封存。内容列必须全空（ck_demos_erased），详情页 404
    ('edge-erased', NULL, NULL, NULL, 'approved', 'published', '2026-08-22 11:30:00+08', '2026-08-22 11:30:00+08')
ON CONFLICT (slug) DO UPDATE SET
    title            = EXCLUDED.title,
    description      = EXCLUDED.description,
    cover_url        = EXCLUDED.cover_url,
    moderation_state = EXCLUDED.moderation_state,
    lifecycle_state  = EXCLUDED.lifecycle_state,
    created_at       = EXCLUDED.created_at,
    updated_at       = EXCLUDED.updated_at;

-- 两条需要额外列的行单独更新（上面那条多行 INSERT 里写不下时间顺序约束）：
--   * 软删：deleted_at 必须 >= created_at
--   * 擦除：erased_at 非空 => deleted_at 必须非空且更早（ck_demos_time_order）
UPDATE demos SET deleted_at = '2026-08-22 12:00:00+08'
WHERE slug = 'edge-soft-deleted' AND deleted_at IS NULL;

UPDATE demos SET deleted_at = '2026-08-22 12:00:00+08', erased_at = '2026-08-22 12:05:00+08'
WHERE slug = 'edge-erased' AND erased_at IS NULL;

-- ===== 渠道行（列表可见性靠它）=====
-- 列表要求「至少一条 visibility='listed' 的渠道行」，所以种子数据也必须造。
-- 只给已发布且审过的行：归档 / 移除 / 软删 / 已擦除的本来就不该出现在作品库里。
INSERT INTO demo_channels (demo_id, channel, visibility, lang, published_at)
-- published_at 留 NULL：这些作品的真实发布时间没有留存，写 now() 是编造，
-- 写 d.created_at 又会撞 ck_channel_time（published_at 必须 >= 渠道行自己的 created_at）。
-- NULL 的语义就是「不知道」，列表也不读这一列。
SELECT d.id, 'deep', 'listed', 'zh', NULL::timestamptz
FROM demos d
WHERE d.lifecycle_state = 'published'
  AND d.moderation_state = 'approved'
  AND d.deleted_at IS NULL
  AND d.erased_at IS NULL
ON CONFLICT (demo_id, channel) DO NOTHING;
