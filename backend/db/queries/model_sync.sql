-- name: DeleteCatalogItemsBySource :exec
-- 清空一个来源的镜像。
--
-- 同步是「整表重写」：镜像只回答「上游现在有什么」。逐条 upsert 会让表里混着
-- 不同时间抓的条目，之后分不清「上游改了」和「这条还没更新」。
--
-- 行 id 会因为重写而整批换新，但没有任何地方引用它：`model_external_refs` 存的是
-- (source, provider_id, external_id) 三元组，导入端点也按三元组查条目。
DELETE FROM model_catalog_items WHERE source = @source::text;

-- name: InsertCatalogItem :copyfrom
-- 批量灌镜像。
--
-- 用 COPY 而不是逐条 INSERT：一次同步约 9000 行，逐条是 9000 次往返。
INSERT INTO model_catalog_items (
    source, provider_id, provider_name, external_id, name, canonical_id, vendor_slug,
    family, description, modalities_in, modalities_out, context_window, max_output,
    cost_in, cost_out, release_date, knowledge_cutoff, open_weights, model_type,
    upstream_status, supports_tool_call, supports_reasoning, supports_structured_output,
    supports_attachment, raw_json, fetched_at
) VALUES (
    @source, @provider_id, @provider_name, @external_id, @name, @canonical_id, @vendor_slug,
    @family, @description, @modalities_in, @modalities_out, @context_window, @max_output,
    @cost_in, @cost_out, @release_date, @knowledge_cutoff, @open_weights, @model_type,
    @upstream_status, @supports_tool_call, @supports_reasoning, @supports_structured_output,
    @supports_attachment, @raw_json, @fetched_at
);

-- name: UpsertModelSourceSync :exec
-- 记账一行：这次同步什么时候试的、成没成、抓到了多少条、payload 指纹是什么。
--
-- 失败时**不更新** `last_success_at` 与 `item_count`：镜像还是上一次成功那份，
-- 这两个值就该停在上一次。否则运维看到「成功时间 = 刚才」，会以为镜像已经更新。
INSERT INTO model_source_sync (source, last_attempt_at, last_success_at, payload_hash, item_count, status, error)
VALUES (
    @source::text,
    now(),
    @last_success_at,
    @payload_hash,
    @item_count,
    @status::text,
    @error::text
)
ON CONFLICT (source) DO UPDATE SET
    last_attempt_at = EXCLUDED.last_attempt_at,
    -- 成功才推 last_success_at；失败保留旧值。
    last_success_at = COALESCE(EXCLUDED.last_success_at, model_source_sync.last_success_at),
    payload_hash    = COALESCE(EXCLUDED.payload_hash, model_source_sync.payload_hash),
    item_count      = COALESCE(EXCLUDED.item_count, model_source_sync.item_count),
    status          = EXCLUDED.status,
    error           = EXCLUDED.error;

-- name: GetModelSourceSync :one
-- 读一个来源的同步状态。从没同步过时返回 ErrNoRows（不返回零值行）：
-- 「有一行但全是零」与「一行都没有」对调用方是两件事，前者会让「上次成功于 0001 年」。
SELECT source, last_attempt_at, last_success_at, payload_hash, item_count, status, error
FROM model_source_sync
WHERE source = @source::text;

-- name: TryModelSyncLock :one
-- 抢一把进程间的锁。
--
-- 定时同步在多实例部署下会同时到点。没有这把锁，两个实例会各拉一次 5.96 MB、
-- 各清一次表——后完成的那次覆盖前一次的记账，而且中间有一段时间表是空的。
--
-- 用 `pg_try_advisory_lock` 而不是 `pg_advisory_lock`：抢不到应该直接跳过这一轮，
-- 而不是排队等——等到的下一轮本来就已经过时了。
SELECT pg_try_advisory_lock(@key::bigint) AS acquired;

-- name: ReleaseModelSyncLock :exec
SELECT pg_advisory_unlock(@key::bigint);

-- name: GetSiteSettingBoolean :one
-- 读一个 boolean 类型的站点参数：有覆盖读覆盖，没有读注册表里的默认值。
--
-- 与 GetSiteSettingNumber 同一形状（缺键直接 ErrNoRows，不返回 false）：
-- 键名拼错时「定时同步永远关着」比「启动就炸」更难发现。
SELECT (COALESCE(s.value, k.default_value))::text::boolean AS value
FROM setting_keys k
LEFT JOIN site_settings s ON s.key = k.key
WHERE k.key = @key::text
  AND k.scope = 'site'
  AND k.value_type = 'boolean'
  AND k.is_active;
