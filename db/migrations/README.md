# 数据库迁移

**改表只有一条路：往 `db/migrations/` 加一个新文件。** 不要改 `db/schema/`，也不要改已经合入的迁移。

## 两段式历史

| 目录 | 角色 | 能不能改 |
| --- | --- | --- |
| `db/schema/*.sql` | **冻结基线**，等价于「0000 号迁移」 | ❌ 永远不改 |
| `db/migrations/*.sql` | 基线之后的所有变化，只增不改 | ❌ 已合入的不改，只能再加一条 |

为什么基线不拆成十条迁移：那是纯改名，会波及 103 处文档引用（`DDL 见 db/schema/10-identity.sql`）。
冻结基线能达到同样的「历史不可变」效果，代价只是接受「历史开头是一份快照」——
真实项目里常规做法（Django 的 squash、Rails 的 schema.rb 都是这个思路）。

## 加一条迁移

1. 文件名 `NNNN_短名.sql`，四位序号，接着当前最大号往下排。
2. 内容只写**这一次的变化**（`ALTER TABLE` / `CREATE INDEX` / `INSERT` 种子…），不写「最终形状」。
3. 生成校验清单：`node scripts/check-migrations.mjs --seal`
4. 跑一遍：`cd backend && DATABASE_URL=… go run ./cmd/migrate up`

## 执行与记录

跑迁移会建一张 `schema_migrations` 表：

```sql
CREATE TABLE schema_migrations (
    version    bigint      PRIMARY KEY,
    name       text        NOT NULL,
    checksum   text        NOT NULL,   -- 文件 sha256
    applied_at timestamptz NOT NULL DEFAULT now()
);
```

- 全新的库：先整块应用基线（登记 `version = 0`），再按序应用迁移。
- 已有的库：只应用「`schema_migrations` 里没有的」。
- **每条迁移和它的登记在同一个事务里**：要么全成，要么全不成，不会出现「结构改了但没登记」的半截状态。

```bash
cd backend
DATABASE_URL=postgres://... go run ./cmd/migrate status   # 看跑到第几版
DATABASE_URL=postgres://... go run ./cmd/migrate up       # 应用待跑的
```

## 为什么「已合入的不许改」

不同机器的结构，只由「它跑过哪些迁移文件」决定。只要有一个文件被事后改动，
两台机器的结构就会**悄悄分叉**——你本地改完重跑没感觉，别人的库已经跑过那一条，永远拿不到修改。

`checksums.json` 就是抓这个的：文件内容变了，校验和对不上，CI 直接红。

## 改错了怎么办

**向前修，不要回滚。** 写一条新迁移把问题改回来（`0011_修掉0010的问题.sql`）。
不提供 `down`：真实项目里 `down` 掉数据比 `up` 出错更常见。

## 会牵动什么

| 改表之后的连带动作 | 说明 |
| --- | --- |
| `sqlc generate` | 查询类型跟着变；CI 会比对生成物 |
| 文档里的字段语义 | 设计文档只写「DDL 见 xxx.sql」，不抄建表语句 |
| 运行手册 | 结构性上限（长度、精度）登记在 `docs/运行手册-可变常量.md` |

## 种子数据不在这里

开发用的假作品放在 [`db/seeds/`](../seeds/)（`npm run db:seed`），**不进迁移链**：
迁移会被带进任何真实库，而种子只有显式执行才会生效。见 [`db/seeds/README.md`](../seeds/README.md)。
