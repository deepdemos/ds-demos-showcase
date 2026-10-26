# 实体域 UML 图集 v1.0

**适用范围**：本文是 [`实体域数据结构设计.md`](./实体域数据结构设计.md) v2.0 的**图形化补充**。只画图，不重复 DDL 与文字论证。所有图基于 v2.0 的三级层次（Task → Prompt → Demo）。

**图目录**：

| # | 图 | 类型 | 回答什么问题 |
|---|---|---|---|
| 1 | 全景类图 | classDiagram | 有哪些实体、什么属性、什么关系 |
| 2 | 数据流图（Level 0） | flowchart | 数据从哪来、到哪去 |
| 3 | 数据流图（Level 1） | flowchart | 每个处理过程内部怎么流 |
| 4 | ER 图（完整） | erDiagram | 表结构 + 外键 + 基数 |
| 5 | 状态机：Task | stateDiagram | 题目的一生 |
| 6 | 状态机：Prompt | stateDiagram | 提示词的一生 |
| 7 | 时序：上传作品 | sequenceDiagram | 从上传到入库 |
| 8 | 时序：从 prompt 簇成题 | sequenceDiagram | 聚类 → 建题 → 归类 |
| 9 | 时序：合并题目 | sequenceDiagram | 两条边怎么迁 |
| 10 | 时序：改 prompt 换题 | sequenceDiagram | 视图的自动效果 |
| 11 | 时序：提示词搜索 | sequenceDiagram | pg_trgm 怎么走 |
| 12 | 组件图 | flowchart | 谁依赖谁 |
| 13 | 活动图：挂题决策 | flowchart | 挂题时的判断链 |
| 14 | 部署图 | flowchart | 表落在哪个库 |

---

## 1. 全景类图

```mermaid
classDiagram
    direction TB

    class Task {
        +bigint id
        +text slug
        +text title
        +text brief
        +text category
        +TaskStatus status
        +bigint merged_into_id
        +bigint created_by
        +timestamptz created_at
        +timestamptz updated_at
        +timestamptz deleted_at
        +prompts() TaskPromptList
        +primaryPrompt() Prompt
        +demos() DemoList
    }

    class TaskPrompt {
        +bigint task_id
        +bigint prompt_id
        +PromptRole role
        +text note
        +bigint created_by
        +timestamptz created_at
    }

    class Prompt {
        +bigint id
        +text content_hash
        +text content
        +bigint created_by
        +timestamptz created_at
        +tasks() TaskPromptList
        +demos() DemoList
        +entries() EntryList
    }

    class PromptLibraryEntry {
        +bigint id
        +bigint prompt_id
        +Scope scope
        +text title
        +text category
        +text source_url
        +bigint created_by
        +timestamptz created_at
        +timestamptz updated_at
        +timestamptz deleted_at
    }

    class Demo {
        +bigint id
        +text slug
        +text title
        +text prompt
        +bigint prompt_id
        +DemoStatus status
        +bigint author_id
        +timestamptz created_at
    }

    class TaskSlugHistory {
        +text slug
        +bigint task_id
        +timestamptz created_at
    }

    class TaskStatus {
        <<enumeration>>
        candidate
        active
        merged
        hidden
    }

    class PromptRole {
        <<enumeration>>
        primary
        variant
    }

    class Scope {
        <<enumeration>>
        deep
        astra
    }

    class DemoStatus {
        <<enumeration>>
        pending
        approved
        rejected
    }

    Task "1" --> "0..*" TaskPrompt : 归类
    Prompt "1" --> "0..*" TaskPrompt : 被归类
    Prompt "1" --> "0..*" PromptLibraryEntry : 收录
    Prompt "1" --> "0..*" Demo : 生成
    Task "1" --> "0..*" TaskSlugHistory : 历史 slug
    Task ..> TaskStatus
    TaskPrompt ..> PromptRole
    PromptLibraryEntry ..> Scope
    Demo ..> DemoStatus
```

**读图要点**：

| 关系 | 基数 | 含义 |
|---|---|---|
| `Task → TaskPrompt` | 1 : 0..* | 一道题可以归类多条 prompt |
| `Prompt → TaskPrompt` | 1 : 0..* | 一条 prompt 可以被归到多道题 |
| `Prompt → Demo` | 1 : 0..* | 一条 prompt 可以生成多件作品 |
| `Prompt → PromptLibraryEntry` | 1 : 0..* | 一条 prompt 在每个视区最多一条收录 |
| `Task → TaskSlugHistory` | 1 : 0..* | 一道题可以有多个历史 slug |

**注意 `Demo → Prompt` 是 `0..*` 而不是 `1..*`**：`demos.prompt_id` 可空，61.5% 的作品无 prompt。

---

## 2. 数据流图（Level 0：上下文图）

```mermaid
flowchart LR
    subgraph EXT["外部实体"]
        U["用户<br/>（作者 / 管理员）"]
        AG["Agent<br/>（上传 API）"]
        V["访客"]
    end

    subgraph SYS["实体域"]
        P0["处理 0<br/>实体域"]
    end

    subgraph STORE["数据存储"]
        DB[("PostgreSQL<br/>tasks / task_prompts<br/>prompts / prompt_library_entries")]
    end

    U -->|"建题 / 归类 prompt<br/>改题 / 合并 / 删题"| P0
    AG -->|"上传作品<br/>（带 prompt）"| P0
    V -->|"浏览题目 / 搜索提示词"| P0
    P0 -->|"题目列表 / 链条视图<br/>提示词详情"| V
    P0 <-->|"读写"| DB
```

**Level 0 只画「谁和系统交互」**，不画内部处理。

---

## 3. 数据流图（Level 1：主要处理）

```mermaid
flowchart TB
    subgraph IN["输入"]
        I1["上传作品<br/>（title + prompt）"]
        I2["管理员建题<br/>（title + brief）"]
        I3["用户投稿提示词<br/>（title + content）"]
        I4["聚类建议<br/>（prompt 簇）"]
    end

    subgraph P["处理"]
        P1["P1 规范化 prompt<br/>+ 算 content_hash"]
        P2["P2 查/建 Prompt"]
        P3["P3 建 Task"]
        P4["P4 归类<br/>（写 task_prompts）"]
        P5["P5 收录<br/>（写 prompt_library_entries）"]
        P6["P6 合并题目"]
        P7["P7 搜索<br/>（pg_trgm）"]
    end

    subgraph OUT["输出"]
        O1["题目列表<br/>（含 prompt_count）"]
        O2["链条视图<br/>（题面 + 证据行）"]
        O3["提示词详情<br/>（属于哪些题）"]
        O4["搜索结果"]
    end

    subgraph STORE["存储"]
        S1[("prompts")]
        S2[("tasks")]
        S3[("task_prompts")]
        S4[("prompt_library_entries")]
    end

    I1 --> P1
    I3 --> P1
    P1 --> P2
    P2 --> S1
    P2 -->|"prompt_id"| P4

    I2 --> P3
    P3 --> S2

    I4 --> P3
    I4 --> P4

    P4 --> S3
    P4 -->|"引用"| S1
    P4 -->|"引用"| S2

    I3 --> P5
    P5 --> S4
    P5 -->|"引用"| S1

    P6 --> S2
    P6 --> S3

    S2 --> O1
    S3 --> O1
    S3 --> O2
    S1 --> O2
    S3 --> O3
    S1 --> P7
    P7 --> O4
```

**关键数据流**：

| 流 | 从 | 到 | 携带什么 |
|---|---|---|---|
| F1 | 上传作品 | P1 | `title` + `prompt` 原文 |
| F2 | P1 | P2 | 规范化后的 `content` + `content_hash` |
| F3 | P2 | P4 | `prompt_id` |
| F4 | 聚类建议 | P3 + P4 | 一组 `prompt_id` + 建议题名 |
| F5 | P4 | `task_prompts` | `(task_id, prompt_id, role, note)` |
| F6 | P5 | `prompt_library_entries` | `(prompt_id, scope, title, ...)` |

---

## 4. ER 图（完整）

```mermaid
erDiagram
    TASKS {
        bigint id PK
        text slug UK
        text title
        text brief
        text category
        text status
        bigint merged_into_id FK
        bigint created_by FK
        timestamptz created_at
        timestamptz updated_at
        timestamptz deleted_at
    }

    TASK_PROMPTS {
        bigint task_id PK,FK
        bigint prompt_id PK,FK
        text role
        text note
        bigint created_by FK
        timestamptz created_at
    }

    PROMPTS {
        bigint id PK
        text content_hash UK
        text content
        bigint created_by FK
        timestamptz created_at
    }

    PROMPT_LIBRARY_ENTRIES {
        bigint id PK
        bigint prompt_id FK
        text scope
        text title
        text category
        text source_url
        bigint created_by FK
        timestamptz created_at
        timestamptz updated_at
        timestamptz deleted_at
    }

    TASK_SLUG_HISTORY {
        text slug PK
        bigint task_id FK
        timestamptz created_at
    }

    DEMOS {
        bigint id PK
        text slug UK
        text title
        text prompt
        bigint prompt_id FK
        text status
        bigint author_id FK
        timestamptz created_at
    }

    USERS {
        bigint id PK
        text username
    }

    TASKS ||--o{ TASK_PROMPTS : "归类"
    PROMPTS ||--o{ TASK_PROMPTS : "被归类"
    PROMPTS ||--o{ PROMPT_LIBRARY_ENTRIES : "收录"
    PROMPTS ||--o{ DEMOS : "生成（可空）"
    TASKS ||--o{ TASK_SLUG_HISTORY : "历史"
    TASKS }o--|| TASKS : "merged_into"
    USERS ||--o{ TASKS : "created_by"
    USERS ||--o{ PROMPTS : "created_by"
    USERS ||--o{ PROMPT_LIBRARY_ENTRIES : "created_by"
    USERS ||--o{ DEMOS : "author_id"
```

**约束标注**：

| 表 | 约束 |
|---|---|
| `tasks` | `UNIQUE (slug) WHERE deleted_at IS NULL` |
| `task_prompts` | `PK (task_id, prompt_id)`；`UNIQUE (task_id) WHERE role='primary'` |
| `prompts` | `UNIQUE (content_hash)` |
| `prompt_library_entries` | `UNIQUE (prompt_id, scope)` |
| `task_slug_history` | `PK (slug)` |

---

## 5. 状态机：Task

```mermaid
stateDiagram-v2
    [*] --> candidate: 建题（默认）
    candidate --> active: 管理员确认
    candidate --> hidden: 管理员下架
    active --> hidden: 下架
    hidden --> active: 重新上架
    active --> merged: 合并到另一题
    candidate --> merged: 合并到另一题
    merged --> [*]: 终态（merged_into_id 指向归宿）
    active --> deleted: 软删除
    hidden --> deleted: 软删除
    candidate --> deleted: 软删除
    deleted --> [*]

    note right of merged
        merged 是终态
        不能从 merged 回到 active
        要「撤销合并」需新建题
    end note

    note right of deleted
        deleted_at 非空
        slug 可被复用
    end note
```

**状态转移的守卫条件**：

| 转移 | 守卫 |
|---|---|
| `candidate → active` | 无（管理员权限即可） |
| `active → merged` | `target.status IN ('active', 'candidate')`；不成环；`source` 未 merged |
| `* → deleted` | 无 `task_prompts`；无反向 `merged_into`；无 pending 建议 |

---

## 6. 状态机：Prompt

```mermaid
stateDiagram-v2
    [*] --> orphan: 首次入库（无归属）
    orphan --> classified: 被归类到某题
    classified --> classified: 再归到另一题
    classified --> orphan: 取消所有归类
    orphan --> published: 被收录到提示词库
    classified --> published: 被收录到提示词库
    published --> published: 改 title / category
    published --> orphan: 取消收录（deleted_at）
    orphan --> [*]: 无引用时 GC
    published --> [*]: 无引用时 GC

    note right of orphan
        orphan = task_prompts 无对应行
        这是常态（61.5% 的作品无 prompt）
    end note

    note right of published
        published = prompt_library_entries
        有未删除的行
    end note
```

**注意**：`Prompt` 的「状态」不是列，而是**由关联推导**：

| 状态 | 判定 |
|---|---|
| `orphan` | `NOT EXISTS (SELECT 1 FROM task_prompts WHERE prompt_id = p.id)` |
| `classified` | `EXISTS (SELECT 1 FROM task_prompts WHERE prompt_id = p.id)` |
| `published` | `EXISTS (SELECT 1 FROM prompt_library_entries WHERE prompt_id = p.id AND deleted_at IS NULL)` |

**为什么不用状态列**：`Prompt` 的正文不可变，状态完全由关联决定。加状态列会引入双写不一致。

---

## 7. 时序：上传作品

```mermaid
sequenceDiagram
    autonumber
    participant U as 用户
    participant API as 上传接口
    participant SVC as 实体服务
    participant DB as PostgreSQL

    U->>API: POST /demos（title + prompt + 文件）
    API->>API: 校验（title 非空 / prompt ≤ 20000）
    API->>SVC: set_demo_prompt(demo, created_by)

    SVC->>SVC: content = normalize(prompt)
    alt content 为空
        SVC->>DB: UPDATE demos SET prompt_id = NULL
    else content 非空
        SVC->>SVC: hash = sha256(content)
        SVC->>DB: SELECT * FROM prompts WHERE content_hash = hash
        alt 命中
            DB-->>SVC: 已有 prompt 行
        else 未命中
            SVC->>DB: INSERT INTO prompts(content, created_by)
            DB-->>SVC: prompt_id
        end
        SVC->>DB: UPDATE demos SET prompt_id = ?, prompt = ?
    end

    SVC-->>API: ok
    API->>DB: INSERT INTO demos(...)
    API-->>U: 201 Created

    Note over SVC,DB: 此时作品已归属某条 prompt<br/>但还没归到任何题目（orphan）
```

**关键点**：

1. **上传不自动归类**。作品有了 `prompt_id`，但 `task_prompts` 还没写 → 作品不属于任何题目
2. **归类是独立操作**（管理员或聚类建议触发）
3. **`prompt` 和 `prompt_id` 双写**（过渡期）

---

## 8. 时序：从 prompt 簇成题

```mermaid
sequenceDiagram
    autonumber
    participant A as 管理员
    participant CL as 聚类服务
    participant DB as PostgreSQL
    participant SVC as 实体服务

    A->>CL: GET /admin/prompt-clusters
    CL->>DB: SELECT demos WHERE status='approved' AND prompt != ''
    DB-->>CL: 235 条 prompt（38.5% 覆盖）
    CL->>CL: 按文本分组（exact）
    CL->>CL: TF-IDF 余弦 ≥ 0.35（similar）
    CL->>DB: SELECT demo_id FROM demo_tasks JOIN tasks WHERE status='active'
    DB-->>CL: 已覆盖的作品集合
    CL-->>A: 簇列表（含 covered 标记）

    A->>SVC: create_task(title, brief)
    SVC->>DB: INSERT INTO tasks(slug, title, brief, status='active')
    DB-->>SVC: task_id

    loop 每个簇内的 prompt
        A->>SVC: attach_prompt(task, prompt_id, role, note)
        SVC->>DB: INSERT INTO task_prompts(task_id, prompt_id, role, note)
    end

    SVC-->>A: 成题完成

    Note over CL,DB: 三级之后，covered 的计算要改：<br/>SELECT DISTINCT d.id FROM demos d<br/>JOIN task_prompts tp ON tp.prompt_id = d.prompt_id<br/>JOIN tasks t ON t.id = tp.task_id<br/>WHERE t.status = 'active'
```

**注意最后一条 Note**：这是 review 时发现的遗漏——`cluster_service.py:52` 的 `covered` 计算依赖 `demo_tasks`，三级之后要改。

---

## 9. 时序：合并题目

```mermaid
sequenceDiagram
    autonumber
    participant A as 管理员
    participant SVC as 实体服务
    participant DB as PostgreSQL

    A->>SVC: merge_task(source, target, reason)
    SVC->>SVC: 校验 source != target
    SVC->>DB: SELECT status FROM tasks WHERE id = target
    DB-->>SVC: target.status
    alt target.status NOT IN ('active', 'candidate')
        SVC-->>A: 422 合并目标须为 active/candidate
    end

    SVC->>SVC: 防环检查（沿 merged_into 链，最多 20 层）
    alt 成环
        SVC-->>A: 422 合并会形成环
    end

    SVC->>DB: SELECT * FROM task_prompts WHERE task_id = source
    DB-->>SVC: source 的 prompt 列表

    loop 每个 prompt
        SVC->>DB: SELECT role FROM task_prompts WHERE (target, prompt)
        alt target 已有该 prompt
            Note over SVC: 保留 target 的 role，跳过
        else target 没有
            alt source 的 role = 'primary' 且 target 已有 primary
                SVC->>DB: INSERT (target, prompt, role='variant')
                Note over SVC: source 的 primary 降级为 variant
            else
                SVC->>DB: INSERT (target, prompt, role=source.role)
            end
        end
    end

    SVC->>DB: DELETE FROM task_prompts WHERE task_id = source
    SVC->>DB: UPDATE tasks SET status='merged', merged_into_id=target WHERE id=source
    SVC->>DB: INSERT INTO audit_log(action='merge', before, after, reason)
    SVC-->>A: 合并完成

    Note over SVC,DB: 与现状的差异：<br/>现状迁 demo_tasks（作品↔题目）<br/>v2.0 迁 task_prompts（题目↔提示词）
```

**primary 冲突的处理**：

| 情况 | 处理 |
|---|---|
| target 无 primary，source 有 | source 的 primary 直接迁过去 |
| target 有 primary，source 无 | 无冲突 |
| target 有 primary，source 也有 | source 的降级为 variant |

---

## 10. 时序：改 prompt 换题（视图的自动效果）

```mermaid
sequenceDiagram
    autonumber
    participant U as 作者
    participant API as 编辑接口
    participant SVC as 实体服务
    participant DB as PostgreSQL

    Note over U,DB: 初始状态：<br/>demo D 的 prompt_id = P1<br/>P1 归在题 T1 下（role='primary'）<br/>所以 D 属于 T1

    U->>API: PATCH /demos/D（prompt = 新文本）
    API->>SVC: set_demo_prompt(D, created_by)
    SVC->>SVC: content = normalize(新文本)
    SVC->>SVC: hash = sha256(content)
    SVC->>DB: SELECT * FROM prompts WHERE content_hash = hash
    alt 命中（已有别人提交过同样文本）
        DB-->>SVC: P2
    else 未命中
        SVC->>DB: INSERT INTO prompts(content, created_by)
        DB-->>SVC: P2
    end
    SVC->>DB: UPDATE demos SET prompt_id = P2 WHERE id = D

    Note over SVC,DB: 此时 D 的 prompt_id 从 P1 变成 P2

    SVC->>DB: SELECT task_id FROM task_prompts WHERE prompt_id = P2
    alt P2 已归类到 T2
        DB-->>SVC: T2
        Note over U,DB: D 自动从 T1 换到 T2<br/>（因为 demo_tasks 是视图）
    else P2 未归类
        DB-->>SVC: 空
        Note over U,DB: D 自动从 T1 脱离<br/>变成 orphan（不属于任何题）
    end

    SVC-->>API: ok
    API-->>U: 200 OK

    Note over U,DB: 这是三级层次的必然结果<br/>也是它的价值：改 prompt = 换题，自动
```

**这个副作用需要 UI 提示**：

> 「修改提示词会改变作品所属的题目。当前属于《T1》，改后将属于《T2》/ 不属于任何题目。」

---

## 11. 时序：提示词搜索

```mermaid
sequenceDiagram
    autonumber
    participant V as 访客
    participant API as 搜索接口
    participant DB as PostgreSQL

    V->>API: GET /prompts?q=科幻坦克
    API->>API: q = "科幻坦克"（4 字符 ≥ 3）

    API->>DB: SELECT id FROM prompts WHERE content ILIKE '%科幻坦克%'
    Note over DB: 走 GIN 索引（pg_trgm）<br/>ix_prompts_content_trgm

    alt 命中
        DB-->>API: prompt_id 列表
        API->>DB: SELECT ... FROM prompts p<br/>LEFT JOIN prompt_library_entries e<br/>LEFT JOIN counts / model_counts / task_counts<br/>WHERE p.id IN (...)
        DB-->>API: 完整行
        API-->>V: 200（含 content_excerpt / demos_total / task_count）
    else 无命中
        DB-->>API: 空
        API-->>V: 200（空列表）
    end

    Note over API,DB: 与现状的差异：<br/>现状走 FTS5 trigram（仅 SQLite）<br/>v2.0 走 pg_trgm（PG 原生）<br/>< 3 字符仍回退全表 ILIKE（与现状一致）
```

**为什么 `< 3` 字符不走索引**：`pg_trgm` 的 trigram 是 3 字符窗口，`< 3` 字符无法生成 trigram。这与现状 FTS5 的行为一致。

---

## 12. 组件图

```mermaid
flowchart TB
    subgraph API["API 层"]
        R1["/tasks<br/>题目列表 / 详情"]
        R2["/prompts<br/>提示词列表 / 详情 / 搜索"]
        R3["/admin/tasks<br/>建题 / 归类 / 合并 / 删题"]
        R4["/admin/prompt-clusters<br/>聚类建议"]
    end

    subgraph SVC["服务层"]
        S1["task_service<br/>建题 / 归类 / 合并 / 删题"]
        S2["prompt_library_service<br/>收录 / 搜索"]
        S3["cluster_service<br/>聚类"]
        S4["matching_service<br/>slugify / TF-IDF"]
        S5["audit_service<br/>审计快照"]
    end

    subgraph DATA["数据层"]
        D1[("tasks")]
        D2[("task_prompts")]
        D3[("prompts")]
        D4[("prompt_library_entries")]
        D5[("task_slug_history")]
        D6[("demos")]
    end

    R1 --> S1
    R2 --> S2
    R3 --> S1
    R4 --> S3

    S1 --> S4
    S1 --> S5
    S1 --> D1
    S1 --> D2
    S1 --> D5

    S2 --> D3
    S2 --> D4

    S3 --> S4
    S3 --> D2
    S3 --> D6

    S1 -.->|"读"| D3
    S2 -.->|"读"| D6
```

**依赖方向**：

| 从 | 到 | 为什么 |
|---|---|---|
| `task_service` | `matching_service` | slugify |
| `task_service` | `audit_service` | 每次写都记审计 |
| `cluster_service` | `matching_service` | TF-IDF 向量化 |
| `cluster_service` | `task_prompts` | 算 `covered` |
| `prompt_library_service` | `demos` | 算 `demos_total` |

**注意**：`task_service` 和 `prompt_library_service` **互不依赖**。它们通过 `prompts` 表间接关联。

---

## 13. 活动图：挂题决策

```mermaid
flowchart TB
    START(["attach_demos(task, demo_ids)"]) --> C1{"task.status<br/>IN ('merged','hidden')?"}
    C1 -->|"是"| E1["409 该题目已合并/下架"]
    C1 -->|"否"| C2["查 demo_ids 的 prompt_id"]
    C2 --> C3{"有 prompt_id<br/>IS NULL 的作品?"}
    C3 -->|"是"| E2["拒绝：无 prompt 无法归类<br/>（或走 demo_task_overrides）"]
    C3 -->|"否"| C4["去重 prompt_id 集合"]
    C4 --> C5["查已归类的 prompt_id"]
    C5 --> C6{"有未归类的?"}
    C6 -->|"否"| E3["返回 0（全部已归类）"]
    C6 -->|"是"| C7["INSERT task_prompts<br/>(role='variant')"]
    C7 --> C8["audit_log(action='attach')"]
    C8 --> DONE(["返回 attached_prompts 数"])

    E1 --> DONE
    E2 --> DONE
    E3 --> DONE
```

**关键判断**：

| 判断 | 位置 | 后果 |
|---|---|---|
| `task.status` | 最前 | 已合并/下架的题不能再挂 |
| `prompt_id IS NULL` | 中间 | 无 prompt 的作品无法归类 |
| 已归类 | 中间 | 幂等，不重复写 |

**为什么「无 prompt 的作品」要拒绝**：

三级层次的前提是「作品通过 prompt 归属题目」。无 prompt 的作品无法建立这个链路。

**兜底方案**（§4.4 已提）：加 `demo_task_overrides` 表，允许直接挂。但推荐先不加。

---

## 14. 部署图

```mermaid
flowchart TB
    subgraph PG["PostgreSQL 18"]
        subgraph SCHEMA["public schema"]
            T1[("tasks")]
            T2[("task_prompts")]
            T3[("prompts")]
            T4[("prompt_library_entries")]
            T5[("task_slug_history")]
            T6[("demos")]
            T7[("demo_models")]
            T8[("models")]
            T9[("users")]
            V1["VIEW demo_tasks"]
            I1["INDEX ix_prompts_content_trgm<br/>（GIN, pg_trgm）"]
        end
        EXT["EXTENSION pg_trgm"]
    end

    subgraph APP["Go 应用"]
        A1["task_service"]
        A2["prompt_library_service"]
        A3["cluster_service"]
    end

    A1 --> T1
    A1 --> T2
    A1 --> T5
    A2 --> T3
    A2 --> T4
    A3 --> T2
    A3 --> T6

    V1 -.->|"派生自"| T2
    V1 -.->|"派生自"| T6
    I1 -.->|"索引"| T3
    EXT -.->|"提供"| I1

    Note over PG: 所有表在同一个库<br/>无跨库事务
```

**部署要点**：

| 项 | 值 |
|---|---|
| 数据库 | PostgreSQL 18 |
| 扩展 | `pg_trgm`（提示词搜索） |
| 视图 | `demo_tasks`（派生，只读） |
| 索引 | `ix_prompts_content_trgm`（GIN） |
| 跨库 | 无 |

---

## 15. 图与文档的对应

| 图 | 对应文档章节 |
|---|---|
| 1 全景类图 | §3 目标数据模型 |
| 2 数据流图 Level 0 | §2 领域边界 |
| 3 数据流图 Level 1 | §4 写入契约 + §5 读取契约 |
| 4 ER 图 | §3 目标数据模型 + §7 不变量 |
| 5 状态机 Task | §3.1 + §4.6 |
| 6 状态机 Prompt | §3.3 + §3.4 |
| 7 时序 上传作品 | §4.7 |
| 8 时序 簇成题 | §4.2 + §6.2 |
| 9 时序 合并题目 | §4.5 |
| 10 时序 改 prompt 换题 | §3.6 + §9 风险 #2 |
| 11 时序 提示词搜索 | §5.6 |
| 12 组件图 | §8 与模型域的接缝 |
| 13 活动图 挂题决策 | §4.4 |
| 14 部署图 | §6 迁移路线 |

---

## 16. 三张图串起来看

如果只看三张图，建议按这个顺序：

```mermaid
flowchart LR
    A["① 全景类图<br/>（§1）<br/>先看有哪些实体"] --> B["② 数据流图 Level 1<br/>（§3）<br/>再看数据怎么流"]
    B --> C["③ 时序 改 prompt 换题<br/>（§10）<br/>最后看最反直觉的副作用"]
```

**为什么这个顺序**：

1. **全景类图**回答「有什么」——`Task` / `TaskPrompt` / `Prompt` / `Demo` 四个实体，三条边
2. **数据流图**回答「怎么动」——上传 → 规范化 → 查/建 prompt → 归类
3. **改 prompt 换题**回答「最反直觉的地方」——改一条 prompt，作品自动换题

**如果只看一张图**：看 §10 的时序图。它把三级层次的核心机制（视图派生）和最大副作用（自动换题）都画出来了。

---

## 附录：图的取舍说明

### 为什么没有画「对象图」

对象图（Object Diagram）是类图的实例化，用于展示某一时刻的具体对象。本文的场景是**设计**而非**调试**，对象图的价值不大。

### 为什么没有画「通信图」

通信图（Communication Diagram）与时序图等价，只是强调对象间的链接而非时间顺序。时序图更直观。

### 为什么没有画「包图」

包图（Package Diagram）用于展示代码组织。本文只做数据结构，不涉及代码结构。

### 为什么状态机只有两张

`Task` 和 `Prompt` 有状态。`TaskPrompt` 和 `PromptLibraryEntry` 没有状态机：

- `TaskPrompt` 的 `role` 是二值枚举，不是状态机
- `PromptLibraryEntry` 的 `deleted_at` 是布尔，不是状态机

### 为什么数据流图分两层

Level 0（上下文图）只画「谁和系统交互」，用于快速理解边界。Level 1 画内部处理，用于理解数据流。再往下（Level 2）会细到每个函数，超出数据结构设计的范围。
