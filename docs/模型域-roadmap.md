# 模型域 Roadmap 与 Milestone 计划

配套设计：**`docs/模型域设计.md` v1.0（2026-10-25 定稿）**。
本文只做「什么时候做什么、先后约束是什么」，不重复设计细节。

状态：**已提交到 GitHub（2026-10-25）**：milestone M3–M9 已创建，issue #21–#42 已建，[#16](https://github.com/deepdemos/ds-demos-showcase/issues/16) 已收敛为总索引。

## 一、编排原则

1. **先修数据，再加功能**：`demo_models` 收不干净之前，往上挂档案和基准只会放大噪音。
2. **每个 milestone 可独立发布、可回滚**；不追求一次性上线。
3. **用户可见价值尽量前置**：M5/M6/M7 各自能单独交付一块可见功能。
4. **不把重构和功能塞进同一个 PR**：重构 PR 的验收是「契约快照不变」，功能 PR 才允许改契约。

## 二、三条硬约束（排序不是偏好）

| # | 约束 | 后果 |
|---|---|---|
| C1 | `demo_models.provider_id` 必须晚于「差集写入」上线 | 旧的全删重建会在每次编辑时抹掉 provider |
| C2 | 目录镜像（B1）与基准导入（C1）必须晚于回填（A2） | 否则按模型聚合时会遇到「只有标签、没有实体」的作品 |
| C3 | 标签归档删除（A6）必须晚于读路径切换（A4）一个发布周期 | 否则分享出去的 `?tag=model:x` 链接会 404 |

## 三、里程碑

### M1 · 基线护栏 ✅

| 项 | 内容 |
|---|---|
| 目标 | 给后续重构加机器门禁：重构不许改契约 |
| 交付 | OpenAPI 契约快照 + 契约测试（#18） |
| 出口 | backend / gate / model-gate 全绿 |
| 状态 | **PR #18 已开，等 review** |

### M2 · 结构重整：identity 域

| 项 | 内容 |
|---|---|
| 目标 | 验证「按域分包」这套做法跑得通，建立搬文件的验收范式 |
| 交付 | `app/domains/identity/` + router 聚合（#20） |
| 出口 | 契约快照不变；全量 pytest 绿 |
| 状态 | **PR #20 已开，stacked 在 #18 上** |

### M3 · 模型关联收敛 I：审计与回填

| 项 | 内容 |
|---|---|
| 目标 | 让 `demo_models` 成为**可信**事实源（此时还不是唯一） |
| 交付 | 审计脚本 A0、双读打点 A1、差集回填 A2 |
| 用户可见 | 无 |
| 出口 | 审计脚本四类差异中「仅标签有」= 0；双读兜底占比有数字 |
| 依赖 | M1 |

**为什么先做这个**：它零风险（只读 + 只补不删），却能给出「到底差多少」的准确数字。没有这个数字，后面所有判断都是猜的。

### M4 · 模型关联收敛 II：写入与读取

| 项 | 内容 |
|---|---|
| 目标 | 写入串行化，读路径统一到实体 |
| 交付 | `DemoModelWriter`（差集）、上传 `models:` 字段、`?tag=model:x` 翻译、facet 改读 `Model` |
| 用户可见 | 编辑作品不再重置模型关联（bug 修复） |
| 出口 | 连续编辑同一作品两次，`demo_models` 与投影都不抖动；旧链接 200 |
| 依赖 | M3 |

### M5 · 模型档案与上游同步

| 项 | 内容 |
|---|---|
| 目标 | 模型页有真实档案；新模型能搜到并一键导入 |
| 交付 | `vendors` / `providers` / `model_specs` / 4 张同步表；目录镜像；字段级 diff 导入页；模型页档案区 |
| 用户可见 | 模态、上下文、参数、价格、发布日期；「档案缺失」显式标注 |
| 出口 | 从「上游出现新模型」到「本站可展示」在后台 3 步内完成；手工字段永不被覆盖 |
| 依赖 | M3（C2）；不依赖 M4 |

### M6 · provider 维度

| 项 | 内容 |
|---|---|
| 目标 | 作品可标注服务方，访客看得到 |
| 交付 | `demo_models.provider_id` + 上传选择器 + 卡片徽章 + 模型页分布 + `?provider=` |
| 用户可见 | 「这个作品是用哪家服务跑的」 |
| 出口 | 老数据全部显示「未标注」且不报错；编辑后 provider 不被抹掉 |
| 依赖 | **M4（硬约束 C1）** + M5（providers 表） |

### M7 · 第三方基准

| 项 | 内容 |
|---|---|
| 目标 | 模型页有跨榜成绩与综合分 |
| 交付 | 基准 4 表 + 导入脚本 + 只读 API + 归一化 + 综合分 + 强弱标记 |
| 用户可见 | 单榜原始分/名次/来源；综合分与被剔除项；强项/弱项 |
| 出口 | 18 条边界用例全过；自报成绩不进综合分 |
| 依赖 | M5（C2）；C2 阶段依赖 M4（C3 归一化按模型聚合） |
| 阻塞 | **需先拍板归一化方案**（百分位 / min-max / 原始分）与数据落地方式 |

### M8 · 标签退场与结构收尾

| 项 | 内容 |
|---|---|
| 目标 | 删掉双写这套历史包袱，拆掉巨型文件 |
| 交付 | 冻结投影 A5、归档删除 A6、拆 `demos.py`（P2）、迁移出 `main.py`（P3） |
| 用户可见 | 无 |
| 出口 | `audit_model_links.py` 长期为 0；`API_CONTRACT.md` 已改；`demos.py` < 400 行 |
| 依赖 | M4 稳定一个发布周期（C3） |

### M9 · 前端减重与弃用清理

| 项 | 内容 |
|---|---|
| 目标 | 清掉已知技术债，评估是否需要换库/拆服务 |
| 交付 | `DemosView` / `UploadView` 逻辑下沉（P5）、删除弃用 Worker（P6）、PostgreSQL 可行性评估（P7） |
| 依赖 | M8 |

## 四、Issue 清单（已创建）

> 标签沿用仓库现有：`refactor` `backend` `frontend` `data-model` `api-contract` `entities` `admin` `testing` `needs-design` `needs-decision`。

**编号对照**（文内 `I-xx` 即 GitHub issue）：

| I-xx | GH | I-xx | GH | I-xx | GH |
|---|---|---|---|---|---|
| I-01 | #21 | I-09 | #29 | I-17 | #37 |
| I-02 | #22 | I-10 | #30 | I-18 | #38 |
| I-03 | #23 | I-11 | #31 | I-19 | #39 |
| I-04 | #24 | I-12 | #32 | I-20 | #40 |
| I-05 | #25 | I-13 | #33 | I-21 | #41 |
| I-06 | #26 | I-14 | #34 | I-22 | #42 |
| I-07 | #27 | I-15 | #35 | | |
| I-08 | #28 | I-16 | #36 | | |

### M3

**I-01 (#21) `[data-model] 模型关联审计脚本（只读）`**
- 标签：`data-model` `testing`
- 内容：`scripts/audit_model_links.py`，用 `match_model` 解析每个作品的 `model:` 标签，与 `demo_models` 对比，输出四类差异 + 指向 deprecated 的链接 + `unspecified` 占比；支持 `--json` 供后续对拍。
- 验收：对生产库只读运行无异常；产出基线报告贴到 issue。

**I-02 (#22) `[data-model] 读取统一「实体优先、标签兜底」并打点`**
- 标签：`data-model` `backend`
- 内容：`serialize_demo` 的 models 字段改为实体优先，兜底分支加计数器日志。
- 验收：兜底占比可从日志统计；契约快照不变。

**I-03 (#23) `[data-model] 回填 demo_models（只补不删）`**
- 标签：`data-model` `backend`
- 依赖：I-01（#21）、I-02
- 内容：差集回填脚本，`source='migration'`；可重复运行；失败整批回滚。
- 验收：I-01 的「仅标签有」= 0；`demo_models` 无新增重复。

### M4

**I-04 (#24) `[data-model] DemoModelWriter：差集写入替换全删重建`**
- 标签：`data-model` `backend`
- 依赖：I-03
- 内容：新 writer（INSERT/UPDATE/DELETE 差集）、上传接入、投影刷新、`source` 标记；旧 `sync_demo_models` 保留一个版本不删。
- 验收：连续编辑不抖动；并发编辑不互相清空；pytest 全绿。

**I-05 (#25) `[api-contract] 上传支持 models 字段（含兼容）`**
- 标签：`api-contract` `backend`
- 依赖：I-04
- 内容：`models: [str | {ref, provider?, note?}]`；`tags` 里的 `model:` 剥离转引用；更新 `API_CONTRACT.md`。
- 验收：两种入参产出等价结果；至少一个模型引用否则 422。

**I-06 (#26) `[data-model] 读路径切换到实体`**
- 标签：`data-model` `backend` `frontend`
- 依赖：I-04
- 内容：`?tag=model:x` → `?model=` 翻译；作品库 facet 改读 `Model` 表；卡片 models 去掉标签兜底。
- 验收：历史分享链接 200；facet 计数与实体一致。

### M5

**I-07 (#27) `[data-model] 建 vendors / providers / model_specs 与同步 4 表`**
- 标签：`data-model` `backend`
- 依赖：I-03
- 内容：建表 + `models.vendor_id` 回填 + `sync_models_from_modelsdev.py` 改为写镜像/vendors/providers。
- 验收：目录条目落库；vendor 从 canonical 前缀自动建；重复刷新幂等。

**I-08 (#28) `[admin] 模型导入与补全后台（字段级 diff）`**
- 标签：`admin` `frontend` `backend`
- 依赖：I-07
- 内容：搜索目录 → 字段级 diff → 导入/补全/按厂商批量；vendor 与 provider 维护入口。
- 验收：`manual` / `locked` 字段默认不可勾选；批量单条失败不影响其他。

**I-09 (#29) `[entities] 候选确认与手动新建接入目录`**
- 标签：`entities` `admin`
- 依赖：I-08
- 内容：candidate 确认面板（关联并确认 / 仅确认 / 合并 / 驳回）、手动新建两段式表单、认领冲突处理、兜底实体禁挂档案。
- 验收：确认后档案正确；冲突有明确提示；fallback 无法挂 refs。

**I-10 (#30) `[entities] 模型页展示档案`**
- 标签：`entities` `frontend`
- 依赖：I-08
- 内容：模态、上下文、参数（未公开显式标注）、价格、发布日期、能力标记、档案完整度徽章。
- 验收：缺值显 `—` 不显 0；深色模式可读。

### M6

**I-11 (#31) `[data-model] demo_models 增加 provider 维度`**
- 标签：`data-model` `api-contract`
- 依赖：I-04（#24）、I-07
- 内容：`provider_id` / `serving_note` 列；writer 按 `model_id` 差集并更新 provider；上传接口 provider 校验。
- 验收：编辑后 provider 保留；未知 provider 返回 422；老数据全 NULL。

**I-12 (#32) `[frontend] provider 选择器与徽章`**
- 标签：`frontend` `entities`
- 依赖：I-11
- 内容：上传页 provider 选择器（官方置顶）、作品卡/详情徽章、`?provider=` 过滤。
- 验收：无 external_ref 的模型不阻塞上传；「未标注」显式显示。

**I-13 (#33) `[entities] 模型页 provider 分布`**
- 标签：`entities` `frontend`
- 依赖：I-11
- 内容：分布表（作品数/均分/票数）、`未标注` 排最后、<3 件隐藏均分、可点进过滤列表。
- 验收：分布不改变社区分；口径说明可见。

### M7

**I-14 (#34) `[data-model] 第三方基准事实层与导入`**
- 标签：`data-model` `backend`
- 依赖：I-07
- 阻塞：数据落地方式待拍板
- 内容：4 表 + 手工 JSON 导入 + 只读 API（列表/详情/单榜）。
- 验收：幂等导入；`coverage=partial` 拒绝归一化；未匹配进收件箱。

**I-15 (#35) `[entities] 归一化与综合分`**
- 标签：`entities` `backend`
- 依赖：I-14（#34）、I-06
- 阻塞：归一化方案待拍板
- 内容：百分位/min-max 计算、去极值、退化标记、被剔除项返回。
- 验收：18 条边界用例；自报版本不进综合分。

**I-16 (#36) `[entities] 强弱标记与 /benchmarks 页`**
- 标签：`entities` `frontend`
- 依赖：I-15
- 内容：中位数 + MAD 标记、按 category 分组、横向对比页。
- 验收：N<4 不标记；文案明确「强项/弱项 ≠ 榜单有错」。

### M8

**I-17 (#37) `[data-model] 冻结并归档 model 标签`**
- 标签：`data-model` `refactor`
- 依赖：I-06（#26）稳定一个发布周期
- 内容：`model_tag_projection=off` → 观察 → 归档到 `model_tag_archive` → 删除；更新 `API_CONTRACT.md`。
- 验收：全站无功能依赖标签；回滚脚本可用。

**I-18 (#38) `[refactor] 拆 demos.py`**
- 标签：`refactor` `backend`
- 依赖：I-17
- 内容：查询 / 上传 / URL 导入 / 删除分四个 service。
- 验收：契约快照不变；`demos.py` < 400 行。

**I-19 (#39) `[refactor] 启动迁移移出 main.py`**
- 标签：`refactor` `data-model`
- 依赖：I-18
- 内容：版本化迁移 runner + seed 分离。
- 验收：旧库升级幂等；启动只做装配。

### M9

**I-20 (#40) `[refactor] 前端页面减重`**
- 标签：`refactor` `frontend`
- 依赖：I-17
- 内容：`DemosView` / `UploadView` 逻辑下沉 composable；`api/index.ts` 按域拆。
- 验收：视图行数下降 ≥ 40%；vitest 全绿。

**I-21 (#41) `[tech-debt] 删除弃用 Worker 后端`**
- 标签：`tech-debt` `infra`
- 内容：删 `frontend/worker/`、`wrangler.toml`、`hono` 依赖。
- 验收：构建通过；无引用残留。

**I-22 (#42) `[tech-debt] 存储与服务化评估`**
- 标签：`tech-debt` `infra`
- 内容：用真实数据评估 PostgreSQL / 独立部署是否必要。
- 验收：给出结论与触发条件，不预设。

## 五、关键路径

```text
M1 ✅ ─ M2 ─ M3 ─┬─ M4 ─┬─ M6
                 │       │
                 └─ M5 ──┴─ M7
                                  └─ M8 ─ M9
```

- **M3 是所有后续的前置**，且它无用户可见收益——容易被跳过，但跳过等于在流沙上盖楼。
- **M5 与 M4 可并行**（前者建新表、后者改写入），但都要 M3 完成。
- **M7 可提前插入**，只要 M5 的表就绪；它不依赖 M6。

## 六、待拍板清单（阻塞对应 milestone）

| 决策 | 阻塞 | 影响 |
|---|---|---|
| 归一化方案：百分位 / min-max / 原始分 | M7 / I-15 | 只影响读取计算，不改表结构 |
| 基准数据落地：手工 JSON / 后台录入 / 抓取 | M7 / I-14 | 影响写入入口与人力成本 |
| 综合分对外名称与是否要 `/benchmarks` 页 | M7 / I-16 | 文案与页面 |
| 访客能看到 provider 分布聚合数，还是只看到单品徽章 | M6 / I-13 | 隐私与呈现（当前按「能看到」设计） |

## 七、GitHub 落地步骤（待执行）

计划用 **milestone = M3–M9**（M1/M2 已有 PR，不单建），issue 用 `I-01`(#21)(#21)–`I-22`(#42)(#42) 的标题与正文。

创建顺序建议：

1. 先建 milestone（M3 → M9，带一句目标说明）。
2. 再建 issue，按 milestone 分组；依赖写进正文的「依赖」小节，暂不用 sub-issue（仓库当前未启用）。
3. 把 #16 的 P0–P7 checklist 与新 issue 对齐：P0/P1 已完成部分回链到 #18/#20，其余逐步迁移到新 issue，避免两套跟踪并存。
4. #16 保留为**总索引**，只维护「阶段 → issue 链接」表，不再承载细节。

命名约定：

```text
milestone: M3 模型关联收敛 I · 审计与回填
issue    : [data-model] 模型关联审计脚本（只读）
```

## 八、与现有 issue #16 的映射

| #16 阶段 | 新落点 |
|---|---|
| P0 护栏 | M1 / #18 ✅ |
| P1 后端分包 | M2 / #20（identity）+ M8 / I-18、I-19（其余域） |
| P2 拆 demos.py | M8 / I-18 |
| P3 迁移出 main.py | M8 / I-19 |
| P4 拆实体服务 | 并入 I-18 之后，未单列（等 M5/M6 稳定后再切） |
| P5 前端减重 | M9 / I-20 |
| P6 清理 Worker | M9 / I-21 |
| P7 评估 | M9 / I-22 |
| —— | M3–M7 为模型域新增，原 #16 未覆盖 |
