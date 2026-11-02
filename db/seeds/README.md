# db/seeds/ —— 开发用种子数据

**这里不是迁移。** `db/migrations/` 是结构历史，会被 `migrate up` 带进任何库；
`db/seeds/` 只是给人看的开发数据，**只有显式 `npm run db:seed` 才会进库**。

## 为什么单独放

- 生产库不该有假作品。种子混进迁移链，迟早有人把它跑到生产上。
- 种子要能反复跑。每条都用 `ON CONFLICT ... DO UPDATE`，改一行再跑一次就生效，
  不用重建库。
- 种子会随页面需求变化。加一个空态就要加一条空描述的作品，这是产品决策，
  不是结构变更，不该占一条迁移。

## 现在有什么

`0001-dev-demos.sql`，30 条作品：

- **24 条已发布**，其中 10 条的名字来自仓库外 `assets/` 里真实可运行的 demo
  （双黑洞合并、沙漏、布料物理、滚动的天空、重力迷宫、魔方……），其余 14 条是
  同风格的补充，用来把分页铺到两页以上；
- **6 条边界行**，专门给页面挑错用的，`slug` 一律以 `edge-` 开头：

| slug | 挑什么错 |
| --- | --- |
| `edge-no-description` | 描述为空 → 页面必须显示「暂无简介」，不能出现空白块 |
| `edge-long-title` | 130 字符标题 → 卡片不能撑破，窄屏不能横向溢出 |
| `edge-archived` | 作者下架 → 列表里不出现，详情页是 **200 墓碑** |
| `edge-removed` | 违规移除 → 同上，但墓碑文案要与归档不同（标准对象 §ST-2） |
| `edge-soft-deleted` | 软删未擦除 → 列表不出现，详情页 **200 墓碑** |
| `edge-erased` | 逻辑封存 → 详情页 **404**，内容列全空（`ck_demos_erased`） |

边界行的存在是有意的：只塞「漂亮数据」，页面在本地看起来完美，一上真实数据就露馅。

## 怎么跑

```bash
export DATABASE_URL=postgres://...
cd backend && go run ./cmd/migrate up     # 先建结构（只需一次）
npm run db:seed                           # 灌种子（可反复跑）
```

`verify-schema.mjs` 也会应用一遍种子并断言：能应用、幂等、6 条边界行都在。
所以**改了 schema 没改种子，CI 会红**。

## 加一条种子

1. 直接改现有的 `.sql`（新文件按 `NNNN-名称.sql` 递增）；
2. 用 `ON CONFLICT (slug) DO UPDATE` 保证幂等；
3. 注意 `ck_demos_time_order`：`deleted_at >= created_at`，`erased_at` 非空时
   `deleted_at` 必须非空且更早——擦除不能凭空发生，必须先删后擦；
4. 跑 `node scripts/verify-schema.mjs` 确认没踩约束。

## 不要做的事

- 不要把种子写成迁移。
- 不要在种子里放真实用户数据（邮箱、头像、token）。
- 不要让页面依赖某条种子的**具体内容**——依赖 `edge-` 那几条的**存在**是可以的，
  依赖「第 3 条标题是什么」不行。
