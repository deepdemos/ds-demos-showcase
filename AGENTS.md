# AGENTS.md — 协作规则

本文件面向所有贡献者（人类与 AI agent），适用于本仓库的全部工作。
仓库：**DeepDemos Showcase**（deepdemos/ds-demos-showcase）。

## 当前状态

- 本仓库正在**完全重构**：后端 Go 1.27 + chi，前端 React 18.3 + Ring UI 7。旧的 Vue 3 + FastAPI 实现只保留在 `main` 历史中，不再维护。
- `dev` 上已有基础脚手架：`backend/`（chi + `/healthz`）、`web/`（Vite 8 + React Router 7 + TanStack Query 5 + Ring UI + Tailwind CSS 4）、Dev Room（分类、分页、浅色/暗色）与 `ui-contract` 检查；尚无业务代码。
- 已定方向：PostgreSQL 18 + sqlc/pgx、OpenAPI 3.1 契约生成、Nginx 静态 + Go API 同域反代、OSS 预签名直传；按里程碑接入。
- 未经讨论不要改架构、加依赖或跳过上面的接入顺序。

## 分支模型（三条长期分支）

**远程仓库只保留 `dev` / `main` / `release` 三条分支，不保留任何临时分支。**

| 分支 | 角色 | 保护 |
| --- | --- | --- |
| `dev` | **开发分支**（默认分支 / 集成分支） | 受 ruleset 保护：禁止直接 push、force-push、删除；只能经 PR 合并，required checks + review 无阻断 |
| `main` | **部署分支**（预发布 / 验收） | 受 ruleset 保护：禁止直接 push、force-push、删除；PR 锁 + required checks + review 无阻断 + 对话全部解决 |
| `release` | **发行分支**（生产 / 版本发行） | 与 `main` 同级保护；只能从 `main` 合入；合入后打 `vX.Y.Z` tag 并部署 |

- 临时分支从 `dev` 切出，命名：`feat/<short-name>`、`fix/<short-name>`、`refactor/<short-name>`、`docs/<short-name>`、`chore/<short-name>`。
- 临时分支**只存在于个人 fork**，不得推送到上游仓库（`deepdemos/ds-demos-showcase`）。
- 合入后删除临时分支（仓库已开启 delete branch on merge）；上游仓库出现任何临时分支即视为违规，须立即删除。
- 任何共享分支都不允许 force-push。

## 变更流转（唯一合法路径）

1. `feat/*` → PR → `dev`：**squash merge**；CI 全绿。
2. `dev` 稳定 → PR → `main`：**merge commit**（保留 dev 历史）；CI 全绿 + review 无阻断。不得跳过 `main` 直接发 `release`。
3. `main` 验收测试通过 → PR → `release`：**merge commit**；合入后在 `release` 上打版本 tag 并部署。
4. 紧急修复：从 `release` 切 `hotfix/*` → PR 回 `release`；合入后必须把同一修复回灌到 `main` 和 `dev`（PR 或 cherry-pick），不允许只改 `release`。
5. 合并方向只能顺流：`feat → dev → main → release`。禁止把 `main` / `release` 反向合并进 `dev`（hotfix 回灌除外）。

## 严格门禁（硬约束）

完整流程见 [docs/development-workflow.md](docs/development-workflow.md)；CI 失败先读该文档。

- 每个 PR 必须关联 issue（`Closes #N` / `Fixes #N` / `Refs #N`）；CI 红或发现 bug 必须先建 issue，再修复。
- 每个 PR 必须经过独立 review，review 结果必须用 `gh pr review` 回复在 PR 上（普通 issue comment 不算）；review 评论必须晚于最新 commit，push 后必须重新 review。有潜在问题禁止合并。不强制其他 owner approve，作者可在无阻断项后自行合并。CI `pr-guard` 会检查该 review 评论。
- 不确定先 ask：需求、边界、技术选型、数据/安全取舍等任何不确定问题，必须先问用户/负责人，不得自行假设或替用户拍板；review 时也必须检查这一点。
- 一个 PR 只做一类修改；修改超过 500 行或 20 个文件会被 `pr-guard` 直接打回。
- 单文件超过 500 行报告过耦合风险（不红）；超过 800 行 `file-size` 直接失败，必须拆分。

## 前端开发规范（硬约束）

### 控件库

- 页面（含页面级组件）**不得直接使用** Ring UI 或任何第三方 UI 控件，只能从自有控件库 `web/src/components/ui` 导入。
- 控件库是页面可用的唯一控件来源；每个控件必须有明确的 props 类型、事件回调和可访问性属性。
- 简单自定义样式使用 Tailwind CSS 4 utilities；复杂样式可用 CSS Modules；Ring UI 只允许在控件库内使用。
- 需要新控件时：先加入控件库并导出，再在页面使用；不允许页面内临时拼装第三方控件。

### Dev Room

- 控件库中的**每一个控件都必须出现在 Dev Room**（站内 `/dev-room`，生产环境不注册该路由），并覆盖其全部公开变体/状态（含 disabled/loading/error 等）。
- 控件注册名格式为 `<控件>/<样式>`（如 `button/primary`）：同一种控件可以登记多种样式，便于扩展；注册名 kebab-case、唯一、稳定，不随文案变化。
- 每个注册项在 `registry.tsx` 中声明 `component`（对应控件库导出）、中文 `name`、`category` 与示例；分类统一在 `controlCategories` 中维护。
- Dev Room 显示注册名并在 DOM 上用 `data-control-id` 暴露；用户与 agent 用它指代控件。
- Dev Room 支持分类筛选、分页与浅色/暗色切换；所有前端文案使用中文。
- 新增或修改控件，必须在同一个 PR 里同步更新 Dev Room 示例、注册名与分类；Dev Room 未登记 = 控件不可用。
- Dev Room 只用于开发和演示。

### 接线

- **Dev Room 之外的所有功能控件必须已接线**：点击/提交/跳转/受控状态必须连接到真实的 handler、路由或 API。
- 禁止占位实现：空 `onClick`、`TODO`、仅 `console.log`、只有样式没有行为的按钮，一律不允许合入。
- 演示用途的未接线控件只允许存在于 Dev Room。

### CI 硬约束（`ui-contract` job）

CI 必须自动拦截以下情况，不依赖人工 review：

1. 页面/功能代码直接 import Ring UI 或第三方 UI 控件（ESLint `no-restricted-imports`，只允许控件库 import）。
2. 控件库导出与 Dev Room 注册项不一致（每个导出至少一个注册项，每个注册项的 `component` 必须已导出；一个控件允许多个注册项）。
3. Dev Room 之外存在未接线控件（AST/自定义 lint 规则检查事件处理器、路由跳转或表单绑定；无法判定时按失败处理）。
4. 新增控件未同时提交 Dev Room 示例。
5. 注册名缺失/格式错误/重复、分类未定义，或 Dev Room 未渲染 `data-control-id`。

以上检查由 CI 的 `ui-contract` job 执行，未通过不允许合并。

## CI 与合并门槛

- 所有 PR 必须 CI 全绿才能合并；`dev` / `main` / `release` 的 required checks：`backend`、`frontend`、`ui-contract`、`file-size`、`pr-guard`（契约生成接入后加 `contract`）。
- CI 失败先读 `docs/development-workflow.md`，按 issue-first 处理，不得绕过。
- 不允许以“跳过 CI”的方式合并，包括管理员；CI 自身坏了先修 CI。
- 新逻辑必须带测试；修 bug 先补能复现的测试。
- PR 的验证方式必须可复现（命令写进 PR 描述）。

## 提交与 PR 规范

- Conventional Commits：`feat|fix|refactor|docs|chore|test|perf|build|ci: 描述`。PR 标题就是 squash 后的提交标题。
- **提交信息不得包含 AI 署名 trailer**：禁止 `Co-authored-by: Copilot <…>`、`Co-authored-by: DeepSeek <…>` 等任何 AI 工具署名。提交作者只写真实参与的人；AI 辅助不进入 commit 元数据。
- 一个 PR 只做一件事；大规模格式化、重命名必须与功能改动分开。
- PR 描述必须关联 issue（`Closes #N` / `Fixes #N` / `Refs #N`），并写清：改了什么、为什么、怎么验证、影响面（API / DB / 部署 / 安全）。
- 不提交构建产物（`web/dist`、二进制、上传文件）；生成代码按目录规则提交，且必须能由 CI 重新生成。

## 代码规范

- Go：`gofmt` / `goimports` 干净；`golangci-lint run` 零告警；错误用 `%w` 包装上下文；可预期错误不 panic；并发代码必须有 race 测试路径。
- TypeScript / React 18：`eslint` + `prettier` 干净；`tsc --noEmit` 通过；组件 PascalCase，hooks 以 `use` 开头；服务端状态统一走 TanStack Query 5，不手写缓存层。
- 样式：简单布局/自定义用 Tailwind utilities，复杂样式用 CSS Modules；不要在页面里绕开控件库直接使用第三方 UI。
- API 以 `api/openapi.yaml`（OpenAPI 3.1）为唯一事实源：先改契约 → 重新生成 → 再改实现；禁止手改生成目录。
- 数据库变更只加新 migration，禁止修改已合入的 migration。
- 依赖：能用标准库 / 现有依赖解决的不加新依赖；新依赖需在 PR 说明理由与体积影响。Ring UI 只允许在控件库内使用。
- 注释只写代码无法表达的“为什么”，用中文；标识符用英文。

## 安全红线（不可协商）

- 不提交密钥、token、生产配置；配置放 `.env`（已在 `.gitignore`）。
- 提交文件里不写只对某一台开发机器成立的路径、用户名、安装位置或运行状态；命令写成通用形式。
- 用户上传内容一律视为不可信：限制类型、大小、解压条目数；预览必须放在独立沙箱域名，不继承主站 Cookie，禁止与主站同源渲染。
- 鉴权、限流、输入校验、SQL 参数化不允许“先跳过，之后补”。
- 不允许删除或弱化安全相关测试与保护配置。

## 文档

- 技术栈决策记录在 README 与本文件；改选型先讨论并同步文档。
- 开发/评审/合并流程以 `docs/development-workflow.md` 为准；CI 失败先读它。
- 架构或流程变化必须同步更新文档。

## 给 AI agent 的硬性约束

- 不得直接推送 `dev` / `main` / `release`；不得跳过 review/CI 直接合并；不得修改或绕过分支保护 / ruleset。
- 遇到任何不确定问题必须先 ask（不要自行假设）；review/合并不确定时同样先问。
- 不得擅自新增依赖、改架构或改已定选型；变更先讨论并同步文档。
- 收尾必须报告：改了哪些文件、跑了哪些检查、还有什么没验证。
