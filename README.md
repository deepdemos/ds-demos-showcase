# DeepDemos Showcase

DeepDemos 展示站的完全重构：React 前端 + Go 后端。旧 Vue 3 + FastAPI 实现保留在 `main` 历史中。
许可证：AGPL-3.0-only。协作规则见 [AGENTS.md](AGENTS.md)。

## 快速开始（前端 / Dev Room）

需要 Node 24.19+ 与 pnpm 11.21+。

```bash
pnpm --dir web install
pnpm --dir web dev
```

- 首页：<http://127.0.0.1:5173/>
- Dev Room（仅开发环境）：<http://127.0.0.1:5173/dev-room>

## 后端

需要 Go 1.27.3+。

```bash
cd backend
go run ./cmd/api
# GET http://127.0.0.1:8080/healthz
```

## 检查

```bash
pnpm --dir web format:check
pnpm --dir web lint
pnpm --dir web typecheck
pnpm --dir web test
pnpm --dir web build
pnpm --dir web check:ui

cd backend && go test ./...
```

## 目录

```text
api/                        OpenAPI 契约（下一步接入生成链）
backend/
  cmd/api/                  HTTP 入口
  internal/platform/        config / httpserver（后续加 db、auth、storage、jobs）
web/
  src/app/                  路由、Provider、布局
  src/components/ui/        控件库（页面唯一控件来源）
  src/dev-room/             Dev Room（站内 /dev-room，生产不注册）
  src/features/             业务功能（空）
  src/lib/                  基础库（QueryClient 等）
  src/routes/               页面
  src/styles/               全局样式
  scripts/check-ui-contract.mjs  ui-contract 检查
.github/workflows/ci.yml    backend / frontend / ui-contract
```

## 控件与 Dev Room 约定

- 页面只能从 `src/components/ui` 取控件，禁止直接 import Ring UI；简单自定义样式用 Tailwind CSS 4，复杂样式可用 CSS Modules。
- 控件注册名格式为 `<控件>/<样式>`（如 `button/primary`）：同一种控件可以登记多种样式；注册名稳定、唯一、kebab-case，已在用的注册名不改名。
- 每个注册项在 `components/ui/registry.tsx` 中带 `component`（对应控件库导出）、中文 `name`、`category` 和 Dev Room 示例；分类由 `controlCategories` 统一维护。
- Dev Room 支持分类筛选、分页（`?category=input&page=2`）以及浅色/暗色切换；页面文案使用中文。
- 每个注册项渲染 `data-control-id="<注册名>"`，用户与 agent 用它精确指代控件；控件库导出的每个控件至少登记一种样式。
- Dev Room 外的控件必须接线；`pnpm --dir web check:ui` 在 CI 中强制组件/注册项/分类/注册名/接线规则。

## 技术栈（已定）

| 层 | 选型 |
| --- | --- |
| 前端 | React 18.3 + TypeScript 6.0 + Vite 8 + React Router 7 + TanStack Query 5 |
| UI | Ring UI 7（`@jetbrains/ring-ui-built`）+ Tailwind CSS 4.3 + CSS Modules |
| 后端 | Go 1.27.3 + chi 5.5 + slog |
| 数据 | PostgreSQL 18（自托管）+ sqlc + pgx |
| 接口 | OpenAPI 3.1，oapi-codegen + orval（下一步接入） |
| 部署 | Nginx 静态 + Go API 同域反代；OSS 预签名直传 |

尚未接入：OpenAPI 生成链、数据库 schema/migration、登录会话、上传、业务页面。下一步先做契约与 schema，不写业务代码。
