# `db/schema` —— 表结构的唯一事实源

这里的 `.sql` 是**唯一**的表结构事实源。设计文档（`docs/`）不再手抄 DDL，只写「这张表为什么长这样」和字段语义；**列、类型、约束、索引、外键、视图、目录表种子**一律以本目录为准。

- 表结构改动 → 只改这里，然后看 `docs/` 里有没有需要跟着改的**语义说明**（不是抄一份 DDL）。
- 文档与 SQL 冲突 → **以 SQL 为准**，同时把文档那句话改掉（否则下一轮又会照着文档改回去）。
- CI 的 `schema` job 会校验目录结构、主键、外键完整性、以及「文档有没有偷偷再手写 `CREATE TABLE`」。

## 应用顺序

文件名前缀就是顺序，`scripts/check-schema.mjs` 会强制它**两位数字、严格递增**（不要求等于 `10·(i+1)`——`15-settings.sql` 就插在 `10-` 与 `20-` 之间，因为设置域的表要在治理域之前建）。

| 文件 | 内容 | 表的归属 |
|---|---|---|
| `10-identity.sql` | 身份域 **17 张**：`accounts` / `credentials` / `account_emails` / `sessions` / `account_marks` / `auth_events` / `profiles` / `avatars` / `username_history` / `groups` / `group_members` / `clients` / `consents` / `external_links` / `outbox_events`，以及 demo 站投影 `users` 与 demo 站会话 `web_sessions`；另有 `auth_events_default` 分区（`PARTITION OF`，门禁不计入表数） | 身份域 |
| `15-settings.sql` | 设置域 **3 张**：`setting_keys`（参数注册表 = 白名单 + 类型 + 默认值）/ `site_settings`（站点级取值）/ `user_preferences`（用户级取值），以及 **19 条**首批参数种子（site 16 + user 3） | 设置域 |
| `20-governance.sql` | 治理域 **10 张**：`roles` / `permissions` / `role_permissions` / `audit_log`（另有 `audit_log_default` 与当月分区 `audit_log_p2026_10` 两个 `PARTITION OF`，门禁不计入表数）/ `audit_actions` / `audit_entity_types` / `gc_kinds` / `gc_queue` / `review_kinds` / `reports`，以及四张目录表的种子 | 治理域 |
| `30-demo.sql` | Demo 域 **13 张**：`demos` / `demo_revisions` / `demo_types`（作品类型主数据，4 行种子）/ `blobs` / `demo_revision_files` / `demo_channels` / `demo_stats` / `demo_ratings` / `demo_timeline` / `demo_slug_history` / `demo_task_overrides` / `demo_capabilities` / `demo_grants` | Demo 域 |
| `40-entity.sql` | 实体域 **7 张**：`tasks` / `task_prompts` / `task_slug_history` / `prompts` / `prompt_library_entries` / `proposals` / `proposal_refs` | 实体域 |
| `50-classification.sql` | 标签域 **14 张**：`classifications` / `classification_dimensions` / `demo_classifications` / `platforms` / `platform_forms` / `demo_revision_platforms` / `platform_configs` / `demo_revision_platform_configs` / `skills` / `demo_revision_skills` / `mcp_servers` / `demo_revision_mcp_servers` / `external_threads` / `external_events` | 标签域 |
| `60-model.sql` | 模型域 **15 张**：`vendors` / `providers` / `models` / `model_aliases` / `model_specs` / `model_spec_fields` / `model_catalog_items` / `model_external_refs` / `model_source_sync` / `model_tag_archive` / `benchmarks` / `benchmark_versions` / `benchmark_metrics` / `benchmark_scores` / `demo_revision_models` | 模型域 |
| `70-views.sql` | **5 个视图**：`demo_tasks` / `demo_platforms` / `demo_platform_configs` / `demo_skills` / `demo_models`（G18：demo 层同名表改视图） | 标签 / 模型域 |
| `80-late-fk.sql` | **全部 76 条外键**（建完所有表后统一 `ALTER TABLE … ADD CONSTRAINT`） | 跨域 |
| `90-extensions.sql` | 需要 contrib 扩展的对象（`pg_trgm` + 模糊检索索引）；缺扩展的环境整块跳过 | 横切 |

## 为什么外键集中在 `80-late-fk.sql`

因为**存在环**，建表顺序不可能同时满足两边：

- `accounts.role → roles`（治理域）与 `audit_log.actor_id → accounts`（身份域）互为先后；
- `demos.current_revision_id → demo_revisions` 与 `demo_revisions.demo_id → demos` 同理。

把外键全部后置有两个副作用，都是想要的：

1. **建表顺序无关**——文件可以按域拆分，新增域只要加一个文件。
2. **跨域边一眼可见**——`80-late-fk.sql` 里每一条都是「谁依赖谁」的实物证据；跨库/跨服务检查时，这条清单就是迁移清单。

代价：**忘加外键不会报错**。所以 `scripts/check-schema.mjs` 会校验每条外键的列与目标表列都真实存在，且建表文件里不允许出现 `REFERENCES`（防止有人顺手写回表内、绕开集中管理）。

## 怎么改

新增一张表 / 一列：

1. 改对应的 `NN-*.sql`（新域就新建文件，按前缀插在合适位置；顺序要求**两位数字、严格递增**，见门禁）。
2. 外键写进 `80-late-fk.sql`，不要写在 `CREATE TABLE` 里。
3. 跑门禁：`node scripts/check-schema.mjs`。
4. 在本地真库上验一次（见下），因为门禁只做静态校验。
5. 文档里如果写了「这张表有哪几列」的说明，同步那句说明；**不要再贴 DDL**。

## 本地验证（真的建一次库）

门禁是静态的，最终仍要在 PostgreSQL 上跑一遍。仓库不引入运行时依赖，用 WASM 版 PG 即可（无需装服务）：

```bash
npm i -D @electric-sql/pglite          # 仅本地，不要提交 package.json 变更时省略 -D 讨论
node scripts/apply-schema.mjs          # 按文件名顺序执行 db/schema/*.sql
```

最后一句会打印 `tables / views / fks / indexes` 计数与 `server_version`。

**当前基线（2026-11-01，`node scripts/check-schema.mjs` 静态计数）：10 个文件 · 79 表 · 5 视图 · 76 外键。**

> 上一版这里写的是 `tables=75 · views=5 · fks=78 · indexes=199`，与实物不符：实测是 **71 条外键**（设置域落地后 75 条，`demo_types` 落地后 76 条），表数也不是 75（设置域落地后 78，`demo_types` 落地后 79）。本版按门禁实测值改正。**`indexes` 门禁不统计，本轮未复核，故不再列数**。真库计数会**多 3 张 = 82**——`auth_events_default`（身份域）+ `audit_log_default` / `audit_log_p2026_10`（治理域）三个分区是 `PARTITION OF` 写法，门禁的解析器不认，不计入 79。

## 与设计文档的对应关系

| 文档 | 对应 SQL |
|---|---|
| [`docs/身份服务模型设计.md`](../docs/身份服务模型设计.md) | `10-identity.sql` |
| [`docs/设置域设计.md`](../docs/设置域设计.md) | `15-settings.sql` |
| [`docs/治理域设计.md`](../docs/治理域设计.md) | `20-governance.sql` |
| [`docs/Demo域设计.md`](../docs/Demo域设计.md) | `30-demo.sql` |
| [`docs/实体域数据结构设计.md`](../docs/实体域数据结构设计.md) | `40-entity.sql` |
| [`docs/标签域数据结构设计.md`](../docs/标签域数据结构设计.md) | `50-classification.sql` |
| [`docs/模型域设计.md`](../docs/模型域设计.md) | `60-model.sql` |
| [`docs/DDL约定.md`](../docs/DDL约定.md) | 命名 / 类型 / 约束 / 生命周期列的口径（不是 DDL 本身） |
