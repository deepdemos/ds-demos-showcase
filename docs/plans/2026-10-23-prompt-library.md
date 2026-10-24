# 提示词库 Implementation Plan

**Goal:** 收录站外提示词，聚合站内同正文作品，支持复制、测试和提交结果。

**Architecture:** 保留 Prompt 作为固定正文标识。新增 PromptLibraryEntry 保存标题、分类、来源和审核状态。投稿复用 EntitySuggestion 收件箱，公开查询只使用已审核条目及当前视区的已审核作品。

**Tech Stack:** Vue 3、TypeScript、FastAPI、SQLAlchemy、SQLite、Vitest、pytest。

## 范围与约定

- 主人选择第一版支持直接收录站外提示词。零作品提示词可以存在。
- 站内已有提示词按固定正文聚合，保留内部格式和大小写。沿用 CRLF 转 LF、去首尾空白的规则。
- 新正文生成独立记录。已有作品继续对应原正文，本期不提供覆盖正文的编辑操作。
- 投稿要求登录，进入现有审核收件箱。来源链接仅保存 HTTP(S) 地址，不抓取网页。
- 公开入口为 `/prompts`、`/prompts/new` 和 `/prompts/:id`。Astra 橱窗的接口白名单保持现状。
- 列表以文字为主，复用本站字号、颜色、间距、硬边和交互令牌。桌面顶栏主入口保持 4 项（作品库、提示词库、探索、搜索），排行榜移到页脚；移动底部导航为首页、作品、提示词、上传、社区、我的，Logo 仍可回首页。
- 同一正文仅能确认第一轮输入相同。详情提示读者同时查看型号、生成轮次、参考素材和人工修改情况。

## 接口契约

列表项 `PromptListItem` 包含 `id`、`title`、`content_excerpt`（最长 480 字）、`content_truncated`、`category`、`source_url`、`source`、`created_at`、`demos_total` 和 `models_total`。详情 `PromptSummary` 在列表字段之外返回完整 `content`。`source` 为 `contributed` 或 `demo`。

| 接口 | 行为 |
|---|---|
| `GET /prompts` | `q/category/sort/has_results/page/page_size` 筛选分页，排序为 `newest` 或 `most_tested`；只返回正文摘要 |
| `GET /prompts/{id}` | 固定正文全文与元数据；不可见记录返回 404 |
| `GET /prompts/{id}/demos` | 同正文的公开作品分页，计数与当前视区一致 |
| `POST /prompts` | 输入 `title/content/category?/source_url?`，返回 `pending` 和 `suggestion_id`；已公开同正文返回 `existing` 和 `prompt_id` |
| `GET /admin/suggestions/{sid}` | 管理端建议详情；收件箱列表对超长 `new_prompt` 正文截断到 480 字，展开全文时按 id 取完整 payload |

标题最多 200 字，正文最多 20,000 字，分类最多 64 字，来源最多 2,048 字。同人重复待审投稿幂等，重复批准不覆盖原元数据。

正文搜索在 SQLite 上使用 FTS5 trigram 索引预筛候选，再在候选集内跑原有 `icontains` 精确校验，保证与全表 LIKE 口径一致；短查询（<3 字符）、索引缺失或候选为空时回退全表 LIKE，不静默漏正文。索引写入与正文同事务，启动时按 `prompt_fts_ready` 标记首灌一次。

## 实施任务

### 1. 后端实体与可见性

修改 `backend/app/models.py`，新增 `backend/app/services/prompt_library_service.py`、`backend/app/routers/prompts.py`，扩展 schemas 和路由注册。

先在 `backend/tests/test_prompt_library.py` 编写未审核、零作品、跨视区、隐藏作品、分页计数和正文区别测试，运行确认失败。实现 SQL 聚合查询后重复运行。详情与作品分页共用可见性检查，GET 不写库。

### 2. 投稿与旧数据兼容

扩展 `suggestion_service.py` 的 `new_prompt` 分支，沿用现有限流、登录和审计。新增窄范围幂等回填，启动时仅补非空且缺少 `prompt_id` 的旧作品，保留已有链接；回填成功后写 `prompt_backfill_links_done` 标记，稳态启动不再扫描，手动重跑仍可调用原函数。测试重复投稿、拒绝、重复批准和二次回填。

### 3. 前端浏览与投稿

新增 `PromptsView.vue`、`PromptDetailView.vue`、`PromptNewView.vue` 和 `PromptLibraryCard.vue`。修改 routes、导航与 i18n。列表支持搜索、分页和复制；详情展示完整正文、来源和同正文作品；投稿展示审核反馈。处理加载、空结果、错误重试、请求过期和动态路由切换。

### 4. 测试结果与审核接入

扩展 `api/types.ts`、`api/index.ts`、`api/mock.ts`、`UploadView.vue`、`AdminInboxSection.vue` 和 `adminLabels.ts`。上传入口使用 `?prompt=<id>`，仅自动填入空白新表单；存在草稿、编辑内容或请求期间输入时保留原内容，并提供显式填入按钮。管理员能查看完整正文及来源后审核。

### 5. 验证与交付

- 后端先跑新用例，再跑全量 `python -m pytest -q`，仅使用测试临时库。
- 前端运行 `npm run typecheck`、`npm run lint`、`npm run lint:css`、`npm run test` 和 `npm run build`。
- 用 tooling 提取后端与前端骨架，合并并验证，更新生成 hash。用 `uiux-tokens.mjs` 更新令牌统计。
- 在隔离本地服务上验证投稿、审核、复制、无结果提示词、既有作品聚合、上传预填和移动布局。
- 复核改动，记录实际结果。发布和线上部署另行处理。
