-- 厂商实体（模型域 §3.4）的读与建。
--
-- 为什么单独一个文件：`models.sql` 已经 900 行，是这个 epic 里所有模型写路径的合并
-- 瓶颈；sqlc 按目录扫描，新开一个文件就能拿到 internal/store/model_vendor.sql.go，
-- 与别人的改动不撞行。
--
-- 本票只落地「就地新建 + 挂载」需要的最少三条读与一条写。改名 / 合并 / 删除不在
-- 范围里（厂商实体的治理入口还没有，见模型域 §3.4）。

-- name: ListVendors :many
-- 手动新建的下拉数据源：全部厂商，按名称排序。
--
-- 不分页也不筛状态：厂商是受控的少量主数据（models.dev 的 lab 一共 77 个），
-- 而这一版还没有「隐藏厂商」的写入方——`vendors.status` 在库里，但至今没人写过。
SELECT id, slug, name
FROM vendors
ORDER BY name, id;

-- name: FindVendorBySlug :one
-- 就地新建前的判重：`vendors.slug` 是唯一键，折出同一个 slug 就是撞车。
--
-- 查回来还要比名称：名字逐字相同 = 同一家（复用，不报错）；名字不同 = 两家厂商撞了
-- 一个键（报错要求改名）。这一步只有人能判断，库判不了。
SELECT id, slug, name
FROM vendors
WHERE slug = $1;

-- name: GetVendorByID :one
-- 「选下拉里已有那家」时按 id 取那一行。取不到由调用方回 400（不是 404）：
-- 这是新建模型的请求体，形状错了不该走得更远。
SELECT id, slug, name
FROM vendors
WHERE id = $1;

-- name: InsertVendor :one
-- 就地新建一家厂商：只收名称，其余列吃默认值（`source='manual'` 写死在这里，
-- 因为它就是「人工建」这件事本身，不是可配置项）。
--
-- 不做 `ON CONFLICT DO NOTHING` + 回读：撞 slug 时调用方要**报错要求改名**。
-- 静默复用会把两家不同的厂商写成一个，而「名字不同」这件事只有人能判断。
INSERT INTO vendors (slug, name, source)
VALUES (sqlc.arg(slug), sqlc.arg(name), 'manual')
RETURNING id, slug, name;
