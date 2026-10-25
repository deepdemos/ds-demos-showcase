# 开发流程与合并门禁

> 本文件是硬规则，不是建议。CI 失败时先读这里，再动代码。
> 分支模型与前端硬约束见 [AGENTS.md](../AGENTS.md)。

## 0. 红线

- 所有改动走 PR，禁止直接 push dev / main / release。
- **每个 PR 必须关联 issue**；没有关联 issue 的 PR 不允许合并。
- **每个 PR 必须经过独立 review**；有潜在问题就禁止合并。
- 一个 PR **只做一类修改**；规模过大直接打回拆分。
- CI 红或发现 bug：**先建 issue，再修复**；PR 必须关联该 issue。
- 不允许自我 approve、不允许绕过 CI / ruleset、不允许删除或弱化门禁。

## 1. Issue first

### 什么时候必须建 issue

- 修 bug、修 CI 红、线上问题。
- 新功能、重构、性能/安全改动、流程改动。
- 任何需要 PR 的改动都应有对应 issue；一个 PR 对应一个 issue（同一类拆分可共用，不同类必须另建）。

### 创建与关联

    gh issue create --title "fix: 描述问题" --body "现象 / 期望 / 验收标准"

PR 描述里用以下任一关键字关联 issue：

    Closes #123
    Fixes #123
    Refs #123

CI 的 pr-guard job 会检查 PR 标题类型、issue 关联和修改规模；不通过直接红。

## 2. 分支、PR 与规模

- 从 dev 切临时分支：feat/*、fix/*、refactor/*、docs/*、chore/*。
- PR 标题必须是单一 Conventional Commit 类型：feat(scope): ...、fix(scope): ...。
- **一个 PR 只允许一类修改**：功能、重构、格式化、文档不能混在一起；review 必须确认这一点。
- 规模门禁（pr-guard 自动检查）：
  - 修改行数（增 + 删）**不得超过 500 行**；
  - 修改文件数**不得超过 20 个**；
  - 超过任意一条 → 直接打回，按“一类一 PR”拆分。

PR 描述至少包含：关联 issue、变更类型/范围、验证方式、风险与未覆盖项、review 结果。

## 3. 本地检查

    pnpm --dir web format:check
    pnpm --dir web lint
    pnpm --dir web typecheck
    pnpm --dir web test
    pnpm --dir web check:ui
    pnpm --dir web build

    node scripts/check-file-size.mjs

    cd backend && go vet ./... && go test ./...

### 单文件行数门禁

CI 的 file-size job 扫描被 git 跟踪的源码文件（.go .ts .tsx .js .jsx .mjs .cjs .css，排除生成代码与 .d.ts）：

- 单文件 **>500 行**：报告“过耦合风险”警告，不红；review 必须关注，能拆则拆。
- 单文件 **>800 行**：直接失败，必须拆分后才能合并。
- 行数按物理行统计；不要用压缩代码/生成代码绕过规则。

## 4. Review（强制）

- 三个长期分支都要求至少 **1 个 approve** 才能合并（GitHub ruleset 强制，不能自我 approve）。
- 所有 review 结果必须回复在 PR 上（gh pr review / gh pr comment；需要本地查看分支时可用 SSH git fetch），不能只在聊天里说“看过了”。

### 盲审流程（agent 可派发子 agent）

1. 派发一个**不继承作者上下文**的子 agent（blind review），只给它：仓库、PR 编号、AGENTS.md 和本文件。
2. 审阅者执行：

       gh pr view <PR> --json title,body,files,additions,deletions
       gh pr diff <PR>
       gh pr checks <PR>

   gh 不可用时可用 SSH 拉取分支后本地 git diff，但结果仍须通过 gh 回复到 PR。

3. 按下面的清单审查，逐条给结论，不允许只写“LGTM”。
4. 只要发现**潜在问题**，就用 --request-changes：

       gh pr review <PR> --request-changes --body "阻断项：...；建议：..."

   没有正式 approve 权限时用 gh pr review <PR> --comment --body "..."，但作者不得据此合并。
5. 修复后重新盲审；直到无阻断项，才允许有权限的 reviewer 执行 --approve。

### Review 清单

- [ ] PR 关联了 issue，issue 与改动一致。
- [ ] 一个 PR 只有一类修改；没有混入无关重构/格式化。
- [ ] 修改行数 ≤500、文件数 ≤20；否则要求拆分。
- [ ] file-size 警告文件是否被触碰；>800 行的文件是否拆分。
- [ ] CI 全绿；backend/frontend/ui-contract/file-size/pr-guard 全过。
- [ ] 有测试；bug 修复有复现测试；没有未使用的占位代码。
- [ ] 安全边界：用户输入、上传、鉴权、SQL、密钥无缺口。
- [ ] 文档/AGENTS 是否同步；没有机器绑定信息（绝对本地路径、用户名等）。
- [ ] 有潜在问题 → request changes，禁止合并。

## 5. CI 门禁

| Job | 作用 |
| --- | --- |
| backend | gofmt / vet / test -race / build |
| frontend | format / lint / typecheck / test / build |
| ui-contract | 控件库、Dev Room、注册名、接线检查 |
| file-size | 单文件 >500 警告，>800 失败 |
| pr-guard | PR 必须关联 issue、单一标题类型、规模 ≤500 行 / ≤20 文件 |

- dev / main / release 的 required checks：backend、frontend、ui-contract、file-size、pr-guard。
- 所有 job 失败时会输出指引，要求先读本文件。
- 处理 CI 红的顺序：
  1. 建 issue（修 bug / 修 CI 本身）；
  2. 从 dev 切分支修复；
  3. PR 用 Fixes #N 关联；
  4. 本地检查 + 等 CI 绿；
  5. 走 review 流程。
- 禁止在 CI 红、review 未通过、issue 未关联时合并；管理员也不例外。

## 6. 合并

满足全部条件才能合并：

1. 关联 issue；
2. CI 全绿（含 required checks）；
3. 至少 1 个独立 approve，且没有 unresolved threads；
4. review 明确“无阻断项”；
5. 一个 PR 只含一类修改，规模未超限。

合并方式：feat/* → dev 用 squash；dev → main、main → release 用 merge commit。合并后删除临时分支，release 打 tag。

## 7. 给盲审子 agent 的模板

    你是独立盲审者，不继承作者上下文。仓库：<repo>，PR：#<N>。
    只读 AGENTS.md、docs/development-workflow.md 和 PR 内容，不要修改代码。
    执行：gh pr view <N> --json ...；gh pr diff <N>；gh pr checks <N>。
    按“Review 清单”逐项检查，重点：是否只做一类修改、是否关联 issue、规模是否超限、
    是否存在潜在 bug/安全/测试/文档问题。
    把结论回复到 PR：gh pr review <N> --request-changes/--comment --body "..."
    只要发现潜在问题就 request changes；没有阻断项才给 approve（无权限则 comment）。
