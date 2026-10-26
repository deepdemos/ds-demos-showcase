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

- 从 dev 切：feat/*、fix/*、refactor/*、docs/*、chore/*；紧急修复从 release 切 hotfix/*。
- PR 标题必须是单一 Conventional Commit 类型（scope 允许字母/数字/点/连字符，可用 !）。
- 一个 PR 只允许一类修改：功能、重构、格式化、文档不能混在一起；review 必须确认。
- pr-guard 规模上限：修改 ≤500 行（增+删）、≤20 个文件；超过直接打回，按“一类一 PR”拆分。
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
    node --test scripts/checks.test.mjs
    cd backend && go vet ./... && go test ./...

### 单文件行数

file-size 扫描 git 跟踪的源码（.go .ts .tsx .js .jsx .mjs .cjs .css），排除 .d.ts；门禁脚本用 node --test 自检：

- 单文件 >500 行：过耦合风险警告（不红），review 必须关注，能拆则拆。
- 单文件 >800 行：直接失败，必须拆分。
- 按物理行统计；不要用压缩/生成代码绕过。

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
- [ ] 修改 ≤500 行、≤20 文件；否则要求拆分。
- [ ] file-size 警告文件是否被触碰；>800 行是否拆分。
- [ ] CI 全绿：backend / frontend / ui-contract / file-size / pr-guard。
- [ ] review 评论晚于最新 commit；push 后已重新 review。
- [ ] 不确定的问题已 ask 并记录；没有未经确认的假设。
- [ ] 有测试；bug 修复有复现测试；无占位/未使用代码。
- [ ] 输入/上传/鉴权/SQL/密钥等安全边界无缺口。
- [ ] 文档/AGENTS 同步；无机器绑定信息。
- [ ] 有潜在问题就 request changes，禁止合并。

## 5. CI 门禁

| Job | 作用 |
| --- | --- |
| backend | gofmt / vet / test -race / build |
| frontend | format / lint / typecheck / test / build |
| ui-contract | 控件库、Dev Room、注册名、接线检查 |
| file-size | 单文件 >500 警告，>800 失败；脚本自检 |
| pr-guard | issue 关联、单一标题、规模 ≤500 行 / ≤20 文件、必须存在晚于最新 commit 的 review 评论 |

- required checks：backend、frontend、ui-contract、file-size、pr-guard（三个长期分支都启用）。
- 所有 job 失败时会输出指引，要求先读本文件。
- CI 红处理顺序：建 issue → 切分支修复 → PR 用 Fixes #N 关联 → 本地检查 + CI 绿 → 走 review。
- CI 红、review 未通过、issue 未关联时禁止合并；管理员也不例外。

## 6. 合并

满足全部条件才能合并：

1. 关联 issue；
2. CI 全绿（required checks）；
3. review 结论已回复在 PR，且明确“无阻断项”；
4. 无 unresolved threads；
5. 只有一类修改，规模未超限。

ruleset 强制合并方式：dev 只允许 squash；main / release 只允许 merge commit。合并后删临时分支，release 打 tag。
