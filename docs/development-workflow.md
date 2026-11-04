# 开发流程与合并门禁

> 硬规则。CI 失败先读本文件，再动代码。分支模型与前端约束见 [AGENTS.md](../AGENTS.md)。

## 0. 红线

- 所有改动走 PR，禁止直接 push dev / main / release。
- 每个 PR 必须关联 issue；没有 issue 不允许合并。
- 每个 PR 必须经过独立 review，并在 PR 上留下晚于最新 commit 的 review 评论；有潜在问题禁止合并。
- 不确定先 ask：需求、边界、选型、数据、安全等任何不确定问题，都必须先问用户/负责人，不得自行假设。
- 一个 PR 只做一类修改；规模过大直接打回拆分。
- CI 红或发现 bug：先建 issue，再修复，PR 关联该 issue。
- review 锁：只能经 PR 合并 + required checks + 对话解决 + review 结论；不强制其他 owner approve，作者可在 review 无阻断后自行合并。
- 不允许绕过 review 流程、CI/ruleset、删除或弱化门禁。

## 1. Issue first

- bug、CI 红、线上问题、功能、重构、安全/性能、流程改动都必须有 issue。
- 每个 PR 对应一个 issue；同一类拆分可共用，不同类必须另建。

    gh issue create --title "fix: 描述问题" --body "现象 / 期望 / 验收标准"

- PR 描述用 Closes #123 / Fixes #123 / Refs #123 关联；建议独立成行，代码块里的关键字不算。
- pr-guard 只做文本关联，不校验 issue 是否存在/OPEN；review 必须人工核对，不一致同样阻断。

## 2. 分支、PR 与规模

- 远程仓库只保留 `dev`（开发）/ `main`（部署）/ `release`（发行）三条长期分支，不保留任何临时分支。
- 从 dev 切：feat/*、fix/*、refactor/*、docs/*、chore/*；紧急修复从 release 切 hotfix/*。
- 临时分支只存在于个人 fork，不得推送到上游仓库；上游出现临时分支即违规，须立即删除。
- PR 标题必须是单一 Conventional Commit 类型（scope 允许字母/数字/点/连字符，可用 !）。
- 一个 PR 只允许一类修改：功能、重构、格式化、文档不能混在一起；review 必须确认。
- pr-guard 只限**改动文件数**：**手写部分** ≤20 个文件；超过直接打回，按“一类一 PR”拆分。
- **pr-guard 不限行数**。行数曾是「可 review 性」的代理，但在本仓库被生成物证明不可靠——一次契约改动会连带重生成上百行机器产物，按总量卡只会逼人砍测试。**单个文件多大由 `file-size` 管**（>500 行警告，>800 行失败），那才是「读不动」的直接判据。
- **生成物不计入文件数**：`backend/internal/api/`（oapi-codegen）、`backend/internal/store/`（sqlc）、`web/src/api/gen/`（orval）三个目录下的文件不参与统计。它们不靠人眼 review，而是由 `contract` job「重新生成后 `git diff --exit-code`」校验，那道门禁比任何行数规则都可靠。往这份白名单加路径 = 声明该目录不再手写，需在 PR 说明里讲清；要放的是手写文件，正确做法是把它挪出去。
- **纯文档 PR 豁免规模上限**：改动全部位于 `docs/` 下时只校验标题类型与 issue 关联，不校验行数与文件数——设计文档天然上千行，按 500 行切会把一份连贯的设计切碎。混入任何非 `docs/` 文件即恢复规模校验（fail-closed）。
- PR 描述至少包含：关联 issue、变更类型/范围、为什么、验证、风险、影响面、review 结果。

## 3. 本地检查

    pnpm --dir web format:check
    pnpm --dir web lint
    pnpm --dir web typecheck
    pnpm --dir web test
    pnpm --dir web check:ui
    pnpm --dir web build
    node scripts/check-file-size.mjs
    node --test scripts/*.test.mjs
    pnpm --dir web test:e2e      # 浏览器冒烟，要求 API + 前端已经起着，见 开发环境.md
    cd backend && go vet ./... && go test ./...

仓库工具（`scripts/` 下的门禁脚本）在**根目录**用 npm 装依赖，只有 `contract` 相关的那几个需要：

    npm ci                       # 首次或 package-lock.json 变了之后
    node scripts/check-contracts.mjs
    node scripts/check-schema.mjs
    node scripts/check-constants.mjs
    node scripts/check-migrations.mjs
    node scripts/check-api-compat.mjs  # 契约兼容，见 api/COMPATIBILITY.md
    node scripts/check-layering.mjs    # 后端分层，见 后端分层约定.md
    node scripts/verify-schema.mjs   # 真库校验，见下

**为什么根目录是 npm、`web/` 是 pnpm**：根目录只有一件事要做——把 `db/schema/*.sql` 灌进一个
PostgreSQL 18 跑断言。唯一依赖是 `@electric-sql/pglite`（WASM 版 PG，零传递依赖），脚本本身是
裸 `node`（无构建、无打包）。给它引一套 pnpm workspace 只会让「跑个门禁」多两步。两套包管理
互不 import、互不影响：`web/` 的 `pnpm-lock.yaml` 和根目录的 `package-lock.json` 各自独立。

本地怎么把库、API、前端跑起来，以及两个本机常见的踩坑（pnpm EPERM / vite 端口被排除），见 [`开发环境.md`](开发环境.md)。

### 单文件行数

file-size 扫描 git 跟踪的源码（.go .ts .tsx .js .jsx .mjs .cjs .css），排除 .d.ts；门禁脚本用 node --test 自检：

- 单文件 >500 行：过耦合风险警告（不红），review 必须关注，能拆则拆。
- 单文件 >800 行：直接失败，必须拆分。
- 按物理行统计；不要用压缩/生成代码绕过。

## 3.1 提交信息

- 提交信息不得包含 AI 署名 trailer：禁止 `Co-authored-by: Copilot <…>`、`Co-authored-by: DeepSeek <…>` 等任何 AI 工具署名。
- 提交作者只写真实参与的人；AI 辅助不进入 commit 元数据。
- review 时发现 AI trailer 视为需要修正的问题（不阻断合并，但要求作者在后续提交中改正，不追溯改写已合入历史）。

## 4. Review（强制）

- dev / main / release 保留 PR 锁：只能经 PR 合并，required checks + 对话解决必须通过；不强制其他 owner approve。
- 所有 review 结论必须用 gh pr review 提交到 PR（--comment / --request-changes；本地查看分支可用 SSH git fetch），普通 issue comment 不算，不能只在聊天里说“看过了”；作者可在无阻断项后自行合并。
- review 评论必须晚于最新 commit；push 后旧 review 失效，必须重新 review。review 提交后重跑 pr-guard（gh run rerun <run-id> --failed）或等下一次 CI，避免旧红色结果留在 required check 上。
- CI 的 pr-guard 会检查 PR 上有晚于最新 commit、body ≥10 字符的 review 评论；不足直接红。
- review 中遇到不确定的问题，必须要求作者先 ask 用户/负责人并记录结论；未经确认的假设视为潜在问题，禁止合并。

### 盲审（agent 可派发子 agent）

1. 派发一个不继承作者上下文的子 agent，只给仓库、PR 编号、AGENTS.md 和本文件。
2. 审阅者执行：

       gh pr view <PR> --json title,body,files,additions,deletions
       gh pr diff <PR>
       gh pr checks <PR>

3. 按清单逐条给结论，不允许只写 LGTM。
4. 只要发现潜在问题就 request changes：

       gh pr review <PR> --request-changes --body "阻断项：...；建议：..."

   没有 approve 权限时用 gh pr review <PR> --comment --body "..." 即可，不强制 --approve。
5. 修复后重新盲审；无阻断项即可合并（作者可自行 squash）。

### Review 清单

- [ ] 关联 issue，issue 与改动一致。
- [ ] 只有一类修改；无混入的无关重构/格式化。
- [ ] 手写部分改动 ≤20 个文件（三个生成目录不计入）；行数不限，但单文件 >500 行会告警，>800 行直接失败。
- [ ] file-size 警告文件是否被触碰；>800 行是否拆分。
- [ ] CI 全绿：backend / frontend / ui-contract / file-size / contract / pr-guard。
- [ ] review 评论晚于最新 commit；push 后已重新 review。
- [ ] 不确定的问题已 ask 并记录；没有未经确认的假设。
- [ ] 有测试；bug 修复有复现测试；无占位/未使用代码。
- [ ] 输入/上传/鉴权/SQL/密钥等安全边界无缺口。
- [ ] 文档/AGENTS 同步；无机器绑定信息。
- [ ] 有潜在问题就 request changes，禁止合并。

## 5. CI 门禁

| Job | 作用 |
| --- | --- |
| backend | gofmt / 分层（import 方向）/ vet / test -race / build |
| frontend | format / lint / typecheck / test / build |
| ui-contract | 控件库、Dev Room、注册名、接线检查 |
| file-size | 单文件 >500 警告，>800 失败；脚本自检 |
| contract | 契约（目录表引用一致）、表结构静态检查、可变常量锚点、迁移门禁、**契约兼容**、**真库校验** |
| pr-guard | issue 关联、单一标题、手写部分 ≤20 文件（生成物不计，行数不限）、必须存在晚于最新 commit 的 review 评论 |
| e2e | 浏览器冒烟（真 Chromium + 真 API + 真 PG）：页面能打开、跳转落点对、代理没漏配。**非必需 check** |

- required checks：backend、frontend、ui-contract、file-size、contract、pr-guard（三个长期分支都启用）。`contract` 自 2026-11-01 起加入（ruleset `protect-dev` / `protect-main-release` 同步更新）。
- **`e2e` 不在 required checks 里**（2026-11-04 加入时就是非必需的）：它要同时拉起 postgres + Go API + vite 三件套，链最长、最易抖。先观察抖动率，稳定后再讨论要不要拦合并；现在红了不挡 PR，但**要有人看**——失败先下载 `e2e-report` 工件里的 `trace.md`。
- 所有 job 失败时会输出指引，要求先读本文件。

### 接口契约

`api/openapi.yaml` 是接口的唯一事实源。改接口的顺序固定，不要反过来：

```bash
# 1. 改契约
$EDITOR api/openapi.yaml
# 2. 重新生成（需要 oapi-codegen 在 PATH 上，安装见 api/README.md）
pnpm gen:api:go
# 3. 再改实现
```

CI 会重新生成一遍并 `git diff --exit-code backend/internal/api`——产物与契约不一致就红。
这条是为了让「禁止手改生成目录」不靠自觉，和真库校验是同一个思路。

### 本地跑集成测试（可选）

后端有依赖 PostgreSQL 的集成测试。**默认不需要装库**：没设 `TEST_DATABASE_URL` 时它们会 skip
并打印原因，`go test ./...` 依然全绿。

想本地真跑：

```bash
docker compose up -d db          # 或自备 PostgreSQL 18
npm run db:up                    # 跑基线 + 待应用的迁移
npm run db:seed                  # 灌开发种子（30 条作品，含 6 条边界行；可反复跑）
TEST_DATABASE_URL=postgres://postgres:postgres@127.0.0.1:5432/deepdemos?sslmode=disable \
  go test ./... -run Integration
```

CI 里用 `services: postgres` 起库（`backend` job），先灌 `db/schema/*.sql` 再跑
`go test -race ./...`，所以**集成测试只在 CI 强制执行**。这是有意的取舍：不为少数人要跑的
测试强迫所有人装数据库。代价是可能出现「本地绿、CI 红」——承担方式就是这一句：
CI 红时先看是不是集成测试，再判断是不是自己的改动引起的。

增删表结构后要同步确认 CI 里灌得进去：`db/schema/*.sql` 是被**逐个文件** `psql -f` 的，
顺序错、缺扩展都会在 `准备测试库` 那一步红。

### contract job 里的三类校验

| | 证明什么 | 不证明什么 |
| --- | --- | --- |
| 静态（`check-contracts` / `check-schema` / `check-constants`） | 文件之间互相对得上：权限键绑全 admin、文档引用的目录表行存在、`db/schema` 与文档里的表名一致、运行手册每个锚点还在 | 数据库**真的会拒绝**某条非法数据 |
| 兼容（`check-api-compat`） | 生成出来的 Go 类型表面与基线相比**没有破坏性变更**：没删字段、没删枚举值、没把可空改必填、没删端点 | 同名改语义（`page` 从 1 起改 0 起）、约束收紧（`maxLength` / `maximum`）、加枚举值导致客户端 `switch` 不穷尽——这三类必须靠 review 看，判定细节见 [`api/COMPATIBILITY.md`](../api/COMPATIBILITY.md) |
| 真库（`verify-schema`） | 把 `db/schema/*.sql` 灌进 PostgreSQL 18（PGlite wasm），逐条断言：白名单外键、值类型、枚举、级联删除、审计路由模板、结构性上限 | 业务代码怎么用这些表；性能；事务边界；`90-extensions.sql`（PGlite 无 contrib，该文件在缺扩展的环境整块跳过） |

写「某个约束一定生效」这类结论时，**只有真库校验算证据**；静态门禁只能证明「这条约束在文档和 SQL 里都写着」。

- CI 红处理顺序：建 issue → 切分支修复 → PR 用 Fixes #N 关联 → 本地检查 + CI 绿 → 走 review。

门禁每一种都有它**证明不了**的东西（`check-constants` 不证明值没变、`check-api-compat` 抓不到同名改语义……）。这些边界集中在 [`已知松点.md`](已知松点.md) 里，发现新的就加一条。
- CI 红、review 未通过、issue 未关联时禁止合并；管理员也不例外。

## 6. 合并

满足全部条件才能合并：

1. 关联 issue；
2. CI 全绿（required checks）；
3. review 结论已回复在 PR，且明确“无阻断项”；
4. 无 unresolved threads；
5. 只有一类修改，规模未超限。

ruleset 强制合并方式：dev 只允许 squash；main / release 只允许 merge commit。合并后删临时分支，release 打 tag。
