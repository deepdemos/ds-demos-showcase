# api/ —— 接口契约

本目录是**接口的唯一事实源**。改接口的顺序固定：

```
改 api/openapi.yaml  →  重新生成  →  再改实现
```

手改生成目录（`backend/internal/api/types.gen.go`、`backend/internal/api/server.gen.go`、`web/src/api/gen/`）会被 CI 打回：
CI 会重新生成一遍并比对，产物与契约不一致就是红的。

## 为什么是 OpenAPI 3.0.3，不是 3.1

选型时写的是 3.1，但**没有验证工具链**。实测 `oapi-codegen` v2.5.0 至今不支持 3.1
（上游 issue [#373](https://github.com/oapi-codegen/oapi-codegen/issues/373)）：

- 3.1 的正确写法 `type: [string, 'null']` 会直接生成失败；
- 退回 3.0 的 `nullable: true` 虽然能过，但会让文件既不是合法 3.1 也不是合法 3.0。

取干净的：**降级到 3.0.3**。3.1 相对 3.0 的主要增益是 webhooks 与 JSON Schema 2020-12
对齐，本项目当前一点没用到，降级不损失表达力。等 `oapi-codegen` 支持 3.1 再升。

## 当前覆盖的端点

| 端点 | 方法 | 用途 | 它验证了什么 |
| --- | --- | --- | --- |
| `/healthz` | GET | 站点可用性 | 无参接口；响应不得带配置值（SET-006） |
| `/api/v1/demos` | GET | 作品列表 | 查询参数、分页（默认 50 / 上限 200）、排序枚举 |
| `/api/v1/demos/{slug}` | GET | 作品详情 | 路径参数、格式校验、404 语义、可空字段 |

前端生成的客户端在 `web/src/api/gen/index.ts`，导出 `getHealthz` / `listDemos` / `getDemo`
三个函数，返回值是 `{ data, status }` 联合类型——按 `status` 收窄即可拿到对应结构。

三个端点覆盖了会反复遇到的形状：无参、查询参数、路径参数、分页、可空字段、错误响应。
**业务实现尚未接入**——目前只到接口与类型。

## 改契约的规矩

改了 `openapi.yaml` 要过一道兼容门禁：`node scripts/check-api-compat.mjs`。
它把生成出来的 Go 类型表面和 [`surface.json`](./surface.json) 比，删字段、删枚举值、
可空变必填、删端点都会红。**判定表、边界（它抓不到什么）、什么时候重签基线，
全在 [`COMPATIBILITY.md`](./COMPATIBILITY.md)。**

## 生成

两侧产物都**入库**，CI 会重新生成并比对，不一致就红。

```bash
npm run gen:api        # 两侧一起
npm run gen:api:go     # 只生成 Go
npm run gen:api:web    # 只生成前端
```

| 侧 | 工具 | 产物 | 版本钉在哪 |
| --- | --- | --- | --- |
| Go | `oapi-codegen` | `backend/internal/api/types.gen.go` + `backend/internal/api/server.gen.go` | 根 `package.json` 的 `gen:api:go` 前面的 `go install` 说明 + 生成文件头部 |
| 前端 | `orval` | `web/src/api/gen/index.ts` | 根 `package.json` 的 `gen:api:web`（`npx --yes orval@8.46.0`） |

`orval` 没有放进 `devDependencies`，是因为它带 83 个传递依赖、会让 lockfile 涨 1600 多行，
过不了 500 行的 PR 上限。版本钉在脚本里同样确定，代价是每次生成要联网拉一次。

## 404 与墓碑

`GET /api/v1/demos/{slug}` 的 404 语义**不是**「查不到」那么简单：

- 曾经公开过、后来被作者归档或被管理员移除 → **200 + 墓碑态**（`title` / `description` 为 null）；
- 从未公开过（草稿 / 待审 / 驳回）或已擦除 → **404**。

这条口径来自 `docs/治理域设计.md` 与标准对象 §ST-2，不要在实现里自己改成「统一 404」。
