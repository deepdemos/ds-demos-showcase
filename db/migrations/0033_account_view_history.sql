-- 0033：账号浏览历史（#426 第二半）。
--
-- 为什么新表而不是复用 demo_views：那张表是**去重计数**，只有「哪天看过」的日期粒度，
-- 而且 viewer_key 把匿名与账号混在同一个键空间里。历史页要按时刻倒序、要能单独清空、
-- 要能按账号关闭记录——三件事都需要一行一个账号的明细。
--
-- 语义：
--   - 一行 = 一个账号对一件作品的**最近一次**浏览；重复看是 UPDATE，不是新行。
--   - 清空 = 删行；关闭记录 = user_preferences 的 history.record=false（只停新增，不删旧）。
--   - 上限由 site_settings / setting_keys 的 history.max_entries_account 决定（默认 200），
--     写入后按最近浏览裁剪；本表不存上限，避免第二份默认值。
--   - 作品被删除 / 擦除时行随外键 CASCADE 消失；迁移不做回填（以前没记过）。

CREATE TABLE account_view_history (
    account_id bigint      NOT NULL,
    demo_id    bigint      NOT NULL,
    viewed_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (account_id, demo_id),
    CONSTRAINT fk_account_view_history_account
        FOREIGN KEY (account_id) REFERENCES accounts (id) ON DELETE CASCADE,
    CONSTRAINT fk_account_view_history_demo
        FOREIGN KEY (demo_id) REFERENCES demos (id) ON DELETE CASCADE
);

-- 按人取最近 N 条：主键前导列已能定位到人，这条索引负责排序。
CREATE INDEX ix_account_view_history_time
    ON account_view_history (account_id, viewed_at DESC);

COMMENT ON TABLE account_view_history IS
    '账号浏览历史（#426）：一行 = 一个账号对一件作品的最近一次浏览。';
