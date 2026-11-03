-- 0008：补 demo 域 16 个引用列的外键。
--
-- 这些列全都能插进不存在的 id，或者删掉父行留下孤儿。逐条在真库上验过
-- （见 issue #185 的表格）。补的时候按「谁是拥有者」分三类：
--
--   ① 作品子行 → ON DELETE CASCADE
--      demo_channels / demo_stats / demo_ratings / demo_timeline /
--      demo_slug_history / demo_grants / demo_revision_files
--      作品没了，挂在它上面的行就没有意义。
--
--   ② 人列 → ON DELETE SET NULL
--      demos.author_id / demo_revisions.created_by / demo_channels.curated_by /
--      demo_ratings.user_id / demo_timeline.actor_id / demo_grants.granted_by
--      人没了，作品与记录要留下（`users` 的注销是 soft delete，行不会真消失，
--      所以 SET NULL 只在强删时兜底）。
--
--   ③ 共享实体引用 → NO ACTION
--      demo_revisions.prompt_id → prompts（内容寻址的共享实体，可能被 50 个作品用着）
--      demo_revision_files.blob_hash → blobs（两列都是 NOT NULL，没法 SET NULL；
--        GC 只在引用数归零后删 blob，这条外键正好把「归零」变成可判定的条件）
--      demo_grants.capability → demo_capabilities（能力下线要先清授权，不能悄悄带走）
--
-- 顺序：先加子表 → 父表的边，最后加同一张表内部的边。实际都指向已存在的表，
-- 顺序不影响结果，按 issue 里的编号排是为了对照方便。

-- ① 作品子行
-- 跑之前先查孤儿：加外键时若存量里已有指向不存在行的值，这条迁移会以
-- `violates foreign key constraint` 失败，而那条报错不告诉你有几行、是哪些值。
-- 下面这段先算一遍，带计数中止——换机器、换数据重放时行为可预期。
DO $$
DECLARE n bigint;
BEGIN
    SELECT count(*) INTO n FROM demo_grants g
        WHERE NOT EXISTS (SELECT 1 FROM demo_capabilities c WHERE c.key = g.capability);
    IF n > 0 THEN
        RAISE EXCEPTION 'demo_grants.capability 有 % 行指向不存在的 demo_capabilities.key，先清理再跑', n;
    END IF;

    SELECT count(*) INTO n FROM demo_revision_files f
        WHERE NOT EXISTS (SELECT 1 FROM blobs b WHERE b.sha256 = f.blob_hash);
    IF n > 0 THEN
        RAISE EXCEPTION 'demo_revision_files.blob_hash 有 % 行指向不存在的 blobs.sha256，先清理再跑', n;
    END IF;

    SELECT count(*) INTO n FROM demo_revisions r
        WHERE r.prompt_id IS NOT NULL
          AND NOT EXISTS (SELECT 1 FROM prompts p WHERE p.id = r.prompt_id);
    IF n > 0 THEN
        RAISE EXCEPTION 'demo_revisions.prompt_id 有 % 行指向不存在的 prompts.id，先清理再跑', n;
    END IF;
END $$;

ALTER TABLE demo_channels ADD CONSTRAINT fk_demo_channels_demo_id78
    FOREIGN KEY (demo_id) REFERENCES demos (id) ON DELETE CASCADE;

ALTER TABLE demo_stats ADD CONSTRAINT fk_demo_stats_demo_id79
    FOREIGN KEY (demo_id) REFERENCES demos (id) ON DELETE CASCADE;

ALTER TABLE demo_ratings ADD CONSTRAINT fk_demo_ratings_demo_id80
    FOREIGN KEY (demo_id) REFERENCES demos (id) ON DELETE CASCADE;

ALTER TABLE demo_timeline ADD CONSTRAINT fk_demo_timeline_demo_id81
    FOREIGN KEY (demo_id) REFERENCES demos (id) ON DELETE CASCADE;

ALTER TABLE demo_slug_history ADD CONSTRAINT fk_demo_slug_history_demo_id82
    FOREIGN KEY (demo_id) REFERENCES demos (id) ON DELETE CASCADE;

ALTER TABLE demo_grants ADD CONSTRAINT fk_demo_grants_demo_id83
    FOREIGN KEY (demo_id) REFERENCES demos (id) ON DELETE CASCADE;

ALTER TABLE demo_revision_files ADD CONSTRAINT fk_demo_revision_files_revision_id84
    FOREIGN KEY (revision_id) REFERENCES demo_revisions (id) ON DELETE CASCADE;

-- ② 人列
ALTER TABLE demos ADD CONSTRAINT fk_demos_author_id85
    FOREIGN KEY (author_id) REFERENCES users (id) ON DELETE SET NULL;

ALTER TABLE demo_revisions ADD CONSTRAINT fk_demo_revisions_created_by86
    FOREIGN KEY (created_by) REFERENCES users (id) ON DELETE SET NULL;

ALTER TABLE demo_channels ADD CONSTRAINT fk_demo_channels_curated_by87
    FOREIGN KEY (curated_by) REFERENCES users (id) ON DELETE SET NULL;

ALTER TABLE demo_ratings ADD CONSTRAINT fk_demo_ratings_user_id88
    FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE SET NULL;

ALTER TABLE demo_timeline ADD CONSTRAINT fk_demo_timeline_actor_id89
    FOREIGN KEY (actor_id) REFERENCES users (id) ON DELETE SET NULL;

ALTER TABLE demo_grants ADD CONSTRAINT fk_demo_grants_granted_by90
    FOREIGN KEY (granted_by) REFERENCES users (id) ON DELETE SET NULL;

-- ③ 共享实体引用（都不带 SET NULL：两列是 NOT NULL，也没道理让引用自己消失）
ALTER TABLE demo_revisions ADD CONSTRAINT fk_demo_revisions_prompt_id91
    FOREIGN KEY (prompt_id) REFERENCES prompts (id);

ALTER TABLE demo_revision_files ADD CONSTRAINT fk_demo_revision_files_blob_hash92
    FOREIGN KEY (blob_hash) REFERENCES blobs (sha256);

ALTER TABLE demo_grants ADD CONSTRAINT fk_demo_grants_capability93
    FOREIGN KEY (capability) REFERENCES demo_capabilities (key);

-- 索引：外键不自动建。`demo_revision_files (revision_id)` 已被 uq_manifest_path 的前导列覆盖，
-- 但 blob_hash 那条没有——GC 反查「这个 blob 还有没有引用」要走它。
CREATE INDEX IF NOT EXISTS ix_demo_revision_files_blob ON demo_revision_files (blob_hash);
