# api/ —— 接口契约

本目录是**接口的唯一事实源**。改接口的顺序固定：

```
改 api/openapi.yaml  →  重新生成  →  再改实现
```

手改生成目录（`backend/internal/api/api.gen.go`、`web/src/api/gen/`）会被 CI 打回：
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

三个端点覆盖了会反复遇到的形状：无参、查询参数、路径参数、分页、可空字段、错误响应。
**业务实现尚未接入**——目前只到接口与类型。

## 生成

Go 侧（产物 `backend/internal/api/api.gen.go`，**入库**）：

```bash
go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.5.0   # 只需一次
pnpm gen:api:go
```

`oapi-codegen` 版本写死在上面的 `go install` 里，与生成文件头部的版本号一致；
升级要重生成一次并 review 差异。

## 404 与墓碑

`GET /api/v1/demos/{slug}` 的 404 语义**不是**「查不到」那么简单：

- 曾经公开过、后来被作者归档或被管理员移除 → **200 + 墓碑态**（`title` / `description` 为 null）；
- 从未公开过（草稿 / 待审 / 驳回）或已擦除 → **404**。

这条口径来自 `docs/治理域设计.md` 与标准对象 §ST-2，不要在实现里自己改成「统一 404」。
