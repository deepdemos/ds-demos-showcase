-- 0027：`model_aliases` 的主键从「alias 单列」改成「(alias, model_id) 复合」。
--
-- 为什么：单列主键把「同一个写法只能指一个模型」变成了写不进去的硬约束，而这条约束本身是
-- 猜的——作者手填的名字撞名是常态，两个模型共用一个写法应该写得进来，由解析规则决定取哪
-- 一条。真正的规则是「允许歧义，同 rank 取小的 `model_id`」，写在 `ResolveModelRefs` 的
-- `DISTINCT ON (ref) ... ORDER BY ref, rank, model_id` 里（`backend/db/queries/models.sql`
-- 有注释）。库层说不清「取小的那一条」这件事，只会把「有歧义」变成「写不进去」。
--
-- 顺带说明：旧约束从来没有真正保证过「同一个写法只指一个模型」——解析用 `lower(a.alias)`
-- 匹配而主键大小写敏感，所以 `DeepSeek` / `deepseek` 两条在老表上就指得向两个模型。
--
-- 主键名确认过是 `model_aliases_pkey`（基线里 `alias text PRIMARY KEY` 是列级写法，没有显式
-- 约束名），改完仍用这个名字，少一处「约束名随人写」的漂移。
--
-- 不会因历史数据失败：`alias` 原本就是主键，`(alias, model_id)` 必然已经唯一。
--
-- 索引 `ix_model_aliases_model` 留着：复合主键首列是 `alias`，按 `model_id` 找「这个模型有
-- 哪些写法」（`ListModelAliases` / `ListRevisionModelAliases`）用不到它。
-- `ck_alias_len` 不动：它管的是单个写法本身的长度，与主键形状无关。
--
-- 两句必须在同一个事务里（迁移执行器本来就保证）；不加 `BEGIN`/`COMMIT`，也不用
-- `CREATE INDEX CONCURRENTLY`（不能在事务里跑，这张表很小，独占锁可忽略）。

ALTER TABLE model_aliases DROP CONSTRAINT model_aliases_pkey;

ALTER TABLE model_aliases ADD CONSTRAINT model_aliases_pkey PRIMARY KEY (alias, model_id);
