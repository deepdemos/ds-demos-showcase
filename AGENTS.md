# AGENTS.md — 协作规则

本文件面向所有贡献者（人类与 AI agent），适用于本仓库的全部工作。
仓库：**DeepDemos Showcase**（deepdemos/ds-demos-showcase）。

## 当前状态

- 本仓库正在**完全重构**：后端重写为 Go，前端重写为 React。旧的 Vue 3 + FastAPI 实现只保留在 `main` 历史中，不再维护。
- `dev` 分支从干净状态开始：目前只有 `LICENSE`（AGPL-3.0-only）和本文件。
- **技术栈尚未定稿**：已确定 Go 后端、React 18 前端、Ring UI 作为基础控件来源；数据库、Go 框架、API 契约、构建工具等通过 ASK 逐项确认。
- 技术栈定稿前不写业务代码、不建脚手架、不提交未确认的技术选型文档。

## 分支模型（三条长期分支）

| 分支 | 角色 | 保护 |
| --- | --- | --- |
| `dev` | 默认分支 / 集成分支 | 约定只通过 PR 合入，CI 必须全绿 |
| `main` | 预发布 / 验收 | 受 ruleset 保护：禁止直接 push、force-push、删除；PR 需 1 人 approve 且对话全部解决 |
| `release` | 生产 | 与 `main` 同级保护；只能从 `main` 合入；合入后打 `vX.Y.Z` tag 并部署 |

- 临时分支从 `dev` 切出，命名：`feat/<short-name>`、`fix/<short-name>`、`refactor/<short-name>`、`docs/<short-name>`、`chore/<short-name>`。
- 合入后删除临时分支（仓库已开启 delete branch on merge）。
- 任何共享分支都不允许 force-push。

## 变更流转（唯一合法路径）

1. `feat/*` → PR → `dev`：**squash merge**；CI 全绿。
2. `dev` 稳定 → PR → `main`：**merge commit**（保留 dev 历史）；CI 全绿 + 1 人 approve。不得跳过 `main` 直接发 `release`。
3. `main` 验收测试通过 → PR → `release`：**merge commit**；合入后在 `release` 上打版本 tag 并部署。
4. 紧急修复：从 `release` 切 `hotfix/*` → PR 回 `release`；合入后必须把同一修复回灌到 `main` 和 `dev`（PR 或 cherry-pick），不允许只改 `release`。
5. 合并方向只能顺流：`feat → dev → main → release`。禁止把 `main` / `release` 反向合并进 `dev`（hotfix 回灌除外）。

## 前端开发规范（硬约束）

### 控件库

- 页面（含页面级组件）**不得直接使用** Ring UI 或任何第三方 UI 控件，只能从自有控件库 `web/src/components/ui` 导入。
- 控件库是页面可用的唯一控件来源；每个控件必须有明确的 props 类型、事件回调和可访问性属性。
- 需要新控件时：先加入控件库并导出，再在页面使用；不允许页面内临时拼装第三方控件。

### Dev Room

- 控件库中的**每一个控件都必须出现在 Dev Room**（站内开发房间页面，形态在技术栈定稿时确认），并覆盖其全部公开变体/状态（含 disabled/loading/error 等）。
- 新增或修改控件，必须在同一个 PR 里同步更新 Dev Room 示例；Dev Room 未登记 = 控件不可用。
- Dev Room 只用于开发和演示；生产环境是否暴露、如何鉴权在脚手架阶段确定。

### 接线

- **Dev Room 之外的所有功能控件必须已接线**：点击/提交/跳转/受控状态必须连接到真实的 handler、路由或 API。
- 禁止占位实现：空 `onClick`、`TODO`、仅 `console.log`、只有样式没有行为的按钮，一律不允许合入。
- 演示用途的未接线控件只允许存在于 Dev Room。

### CI 硬约束（`ui-contract` job，脚手架时落地）

CI 必须自动拦截以下情况，不依赖人工 review：

1. 页面/功能代码直接 import Ring UI 或第三方 UI 控件（ESLint `no-restricted-imports`，只允许控件库 import）。
2. 控件库导出清单与 Dev Room 登记清单不一致（机读 registry / manifest 比对，任一方向缺失都失败）。
3. Dev Room 之外存在未接线控件（AST/自定义 lint 规则检查事件处理器、路由跳转或表单绑定；无法判定时按失败处理）。
4. 新增控件未同时提交 Dev Room 示例。

在上述检查落地前，前端控件相关 PR 不允许合并。

## CI 与合并门槛

- 所有 PR 必须 CI 全绿才能合并；`main` / `release` 的 required checks 在 CI 落地后配置（至少包含 `backend`、`frontend`、`contract`、`ui-contract`）。
- 不允许以“跳过 CI”的方式合并，包括管理员；CI 自身坏了先修 CI。
- 新逻辑必须带测试；修 bug 先补能复现的测试。
- PR 的验证方式必须可复现（命令写进 PR 描述）。

## 提交与 PR 规范

- Conventional Commits：`feat|fix|refactor|docs|chore|test|perf|build|ci: 描述`。PR 标题就是 squash 后的提交标题。
- 一个 PR 只做一件事；大规模格式化、重命名必须与功能改动分开。
- PR 描述写清：改了什么、为什么、怎么验证、影响面（API / DB / 部署 / 安全）。
- 不提交构建产物（`web/dist`、二进制、上传文件）；生成代码按目录规则提交，且必须能由 CI 重新生成。

## 代码规范

- Go：`gofmt` / `goimports` 干净；`golangci-lint run` 零告警；错误用 `%w` 包装上下文；可预期错误不 panic；并发代码必须有 race 测试路径。
- TypeScript / React 18：`eslint` + `prettier` 干净；`tsc --noEmit` 通过；组件 PascalCase，hooks 以 `use` 开头；服务端状态统一走数据请求库（定稿后明确），不手写缓存层。
- API 以统一契约为唯一事实源（定稿后明确路径）：先改契约 → 重新生成 → 再改实现；禁止手改生成目录。
- 数据库变更只加新 migration，禁止修改已合入的 migration。
- 依赖：能用标准库 / 现有依赖解决的不加新依赖；新依赖需在 PR 说明理由与体积影响。Ring UI 只允许在控件库内使用。
- 注释只写代码无法表达的“为什么”，用中文；标识符用英文。

## 安全红线（不可协商）

- 不提交密钥、token、生产配置；本地配置放 `.env`（脚手架落地时加入 gitignore）。
- 用户上传内容一律视为不可信：限制类型、大小、解压条目数；预览必须放在独立沙箱域名，不继承主站 Cookie，禁止与主站同源渲染。
- 鉴权、限流、输入校验、SQL 参数化不允许“先跳过，之后补”。
- 不允许删除或弱化安全相关测试与保护配置。

## 文档

- 技术栈定稿后，决策记录写入本文件或独立设计文档；定稿前不推送未确认的设计文档。
- 架构或流程变化必须同步更新文档。

## 给 AI agent 的硬性约束

- 不得直接推送 `main` / `release`；不得修改或绕过分支保护 / ruleset。
- 技术栈未定稿前，不得新增依赖、写脚手架或业务代码，也不得自行替用户拍板未确认的选型。
- 收尾必须报告：改了哪些文件、跑了哪些检查、还有什么没验证。
