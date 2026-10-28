# DDL 约定（PostgreSQL 18）

> **全仓唯一事实源。** 所有域的 DDL、字段表与 ER / 类图一律按本文书写——目前落 DDL 的文档：《标签域数据结构设计》《实体域数据结构设计》《模型域设计》《身份服务模型设计》《论坛外置选型对比》，以及用字段表描述结构的《Demo域设计》与《目标设计/Demo域》。
>
> **域文档只在有偏差时覆盖本文**（目前无偏差）；不要在域文档里另立一套类型映射或表写法。
>
> 建立于 2026-10-28（台账 **C2**：方言由 SQLite 统一为 PostgreSQL 18）。

## 1. 方言

**PostgreSQL 18**，不写其他方言的 DDL。

从旧栈（SQLite）迁入时的「原类型 → 现类型」对照见[附录 A](#附录-a-从-sqlite-迁入的对照c2-迁移期参考)——那是**迁移期参考**，不是现行规则；新表一律按 §2 取类型。

## 2. 语义 → 类型

| 语义 | 落库类型 | 典型列 |
|---|---|---|
| 主键 | `bigint` + `GENERATED ALWAYS AS IDENTITY` | 全部 `id` |
| 外键 / 行计数 / 字节数 | `bigint` | `*_id`、`*_count`、`*_bytes`、`size` |
| 有序号或有界小范围 | `int`（PG 里 `int` ≡ `integer`） | `rev_no` / `sort` / `tier` / `rank` / `score` / `gen_rounds` / `gen_minutes` |
| 0/1 标志 | `boolean`（默认 `false`） | `featured` / `is_harness` / `is_primary` / `locked` |
| 字符串 | `text` | 全部字符串列 |
| 时间点 | `timestamptz` | `created_at` / `published_at` / `deleted_at` |
| 比率 / 小数 | `numeric(p,s)` | `rating_avg`（1–5 分，`numeric(3,2)`） |
| 大整数浮点 | `double precision` | 基准分数 |
| 结构化数据 | `jsonb` | 提案 `payload` |

## 3. 字段表写法

- 「类型」列**只写类型名**；PK / FK / 唯一性写在类型名之后：`bigint PK`、`bigint FK→demos`。
- **自增**不写 `AUTOINCREMENT`：由 DDL 层的 `GENERATED ALWAYS AS IDENTITY` 表达；字段表的「默认」列写「自增」。
- **字符串长度**：统一 `text`，上限写进注释（`（≤128 字符）`）或落库时用 `CHECK (length(x) <= n)`；**不用 `varchar(n)`**。
- **时间列**：`timestamptz`，建表一律 `NOT NULL DEFAULT now()`（可空列除外；事件时间如 `fetched_at` / `last_attempt_at` 不得给默认值）。
- **标志位**：`boolean`，不用 `integer 0/1`（旧栈 SQLite 没有 boolean 类型，是历史包袱）。
- **JSON**：`jsonb`，不用 `text` 存 JSON 再在应用层解析。

## 4. 图与 DDL 的关系

- **类图与 ER 的属性类型与 DDL 同名**：`+bigint` / `text` / `timestamptz` / `boolean` / `numeric`。
- **mermaid `erDiagram` 的属性类型是单 token**：`double precision` 在 ER 里写 `float8`（PG 同义名），否则会被解析成「类型 `double`、列名 `precision`」。
- **视图实体不标 `PK` / `FK`**：视图没有键约束，键语义只在底表上（G18 的 demo 级视图、G7 的 `demo_tasks` 一律如此）。
- **视图无索引、不可写**：写路径一律落到它的底表；ER 里视图与实体之间用虚线（`||..o{`）表示「派生」。

## 5. 索引与约束命名（现状写法，未单独立规）

- **索引**：`ix_<表或缩写>_<列或用途>` —— 全库 **54** 个索引名一致，例：`ix_models_vendor`、`ix_demo_revision_models_model`、`ix_prompts_content_trgm`、`ix_ext_threads_subject`。
- **CHECK**：`ck_<表>_<语义>` —— 现有 5 个：`ck_proposals`、`ck_proposals_payload`、`ck_proposals_review`、`ck_classifications_shape`、`ck_proposal_refs_exactly_one`。
- **主键 / 外键 / 唯一约束不单独命名**：内联 `PRIMARY KEY (...)` / `REFERENCES ...` / `UNIQUE (...)`。

## 6. 各域怎么引用

域文档开头写一句指回本文：

> DDL 约定见 [`DDL约定.md`](./DDL约定.md)（方言、语义→类型映射、字段表写法、图与 DDL 的关系）。

有偏差时在该域显式写「**本域偏离**：… + 理由」，不要静默改写法。目前所有域文档均无偏差。

## 附录 A 从 SQLite 迁入的对照（C2 迁移期参考）

旧栈（`main`，SQLite）的列类型 → 现行类型。**只在迁移辨认旧列时使用**；新表按 §2 取类型。

| 旧（SQLite） | 现（PostgreSQL 18） | 说明 |
|---|---|---|
| `INTEGER PRIMARY KEY AUTOINCREMENT` | `bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY` | 主键 |
| `INTEGER`（FK / 行计数 / 字节数 / id 类） | `bigint` | |
| `INTEGER`（有序号或有界小范围） | `int` | PG 里 `int` ≡ `integer` |
| `INTEGER`（0/1 标志） | `boolean`（默认 `false`） | 语义变化：`=1` / `=0` 的行文与部分索引谓词要同步改 |
| `TEXT` / `TEXT(n)` | `text`（长度上限进注释或 CHECK） | 不用 `varchar(n)` |
| `DATETIME` / `TIMESTAMP` | `timestamptz`，`NOT NULL DEFAULT now()` | |
| `DATE` | `date` | |
| `REAL` | `double precision`（ER 里写 `float8`） | |
| 数值文本存 JSON | `jsonb` | |

> 例外：**旧栈现状快照**（各文档「附录 A 现状字段清单」）里的 `Integer` / `String(n)` / `DateTime` 是 **SQLAlchemy ORM 类型名**，描述的是 `main` 分支的真实结构——**不要按上表去「修正」它**，也不要拿它建表。
