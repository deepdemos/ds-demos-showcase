-- 版本分离：把「对外服务的版本」与「作者手上最新的版本」拆成两个指针。
--
-- # 为什么现在要做
--
-- 在此之前，`demos.current_revision_id` 同时承担两件事：作者编辑的目标、
-- 以及所有公开读路径（列表 / 详情 / 预览 / 下载）的内容来源。这在「一件作品
-- 只有一个版本、且审核态全局一次」的世界里是自洽的。
--
-- 但接上「更新作品」之后它就不成立了：作者改一版，审核态必须回到 `pending`
-- （否则更新无需审核即可上线），而 `pending` 一旦直接决定可见性，已发布的作品会
-- 在复审期间整体下线——用户看到的是「作品突然消失」，而不是「内容还没更新」。
--
-- 拆成两个指针之后语义变清晰：
--   current_revision_id   —— 最新版本，作者看到的、编辑的目标、审核的对象
--   published_revision_id —— 对外服务的版本；NULL 表示这件作品从没有过对外内容
--
-- 于是：更新 → current 指向新版本、moderation 回 pending，公开侧仍读 published，
-- 内容不变；审核通过 → published 指向新版本，公开侧换成新内容。审核驳回 →
-- published 不动，作者知道自己这一版没过，线上仍是上一版。
--
-- # 为什么不是「每个 revision 各有一份审核态」
--
-- DEMO-021 写明审核态全局一次。本条改动不推翻它：作品仍然只有一个审核态，
-- 只是「审核态描述当前版本」，而「对外的版本」由 published_revision_id 记录。
-- 两者是不同的问题，之前挤在一个布尔判断里才显得冲突。

ALTER TABLE demos ADD COLUMN published_revision_id bigint;

COMMENT ON COLUMN demos.published_revision_id IS
    '对外服务的版本。NULL = 从未对外过。公开读路径一律读这一列的版本，不读 current_revision_id。';

-- 回填：已经审过的作品，当前版本就是它在对外服务的版本。
-- 未审过的（pending / rejected）保持 NULL —— 它们从来没有对外过。
UPDATE demos
SET published_revision_id = current_revision_id
WHERE moderation_state = 'approved'
  AND current_revision_id IS NOT NULL;

ALTER TABLE demos
    ADD CONSTRAINT fk_demos_published_revision
    FOREIGN KEY (published_revision_id) REFERENCES demo_revisions (id)
    ON DELETE SET NULL;

CREATE INDEX ix_demos_published_revision ON demos (published_revision_id);

-- ---------- 守卫：published_revision_id 必须属于本作品 ----------
--
-- 与 current_revision_id 同一个道理（0009 的 #2）：没有这条时，A 的对外内容
-- 可以是 B 的版本，而所有公开读路径都会照读，且看起来一切正常。

CREATE OR REPLACE FUNCTION trg_demos_guard() RETURNS trigger AS $$
DECLARE
    owner bigint;
    listed int;
BEGIN
    -- #2：current_revision_id 的 demo_id 必须等于本行 id。
    --
    -- `owner IS NULL` 放行：那是「被指向的版本已经不在了」——删除版本行时
    -- 外键（ON DELETE SET NULL）会把指针清空，而这个 UPDATE 同样会走到本守卫。
    -- 不放行的话，删版本行会被自己的守卫拦住，报「属于作品 <NULL>」。
    IF NEW.current_revision_id IS NOT NULL THEN
        SELECT demo_id INTO owner FROM demo_revisions WHERE id = NEW.current_revision_id;
        IF owner IS NOT NULL AND owner <> NEW.id THEN
            RAISE EXCEPTION 'current_revision_id=% 属于作品 %，不属于作品 %',
                NEW.current_revision_id, owner, NEW.id
                USING ERRCODE = 'restrict_violation';
        END IF;
    END IF;

    -- 同一条守卫，给对外版本指针。
    IF NEW.published_revision_id IS NOT NULL THEN
        SELECT demo_id INTO owner FROM demo_revisions WHERE id = NEW.published_revision_id;
        IF owner IS NOT NULL AND owner <> NEW.id THEN
            RAISE EXCEPTION 'published_revision_id=% 属于作品 %，不属于作品 %',
                NEW.published_revision_id, owner, NEW.id
                USING ERRCODE = 'restrict_violation';
        END IF;
    END IF;

    -- #10 的方向从「审核态」改为「对外版本」：
    -- 有上架中的渠道，就必须有一个对外版本在服务，否则渠道指向的是一条空内容。
    -- 旧写法拦「approved → 非 approved」，那在版本分离之后会挡住更新（更新本来
    -- 就要把审核态退回 pending，而对外内容并没有消失）。
    -- 只在**从有变没有**时拦：`IS NULL` 一个条件会把「本来就没有」的行也挡住，
    -- 那些行只是来改个标题、改个删除时间——拦住它们等于让一条残缺数据永远修不了
    -- （种子重跑就是这样撞上的：旧库里渠道行还在、指针还没补，任何 UPDATE 都过不去）。
    IF TG_OP = 'UPDATE'
       AND OLD.published_revision_id IS NOT NULL
       AND NEW.published_revision_id IS NULL THEN
        SELECT count(*) INTO listed
        FROM demo_channels WHERE demo_id = NEW.id AND visibility = 'listed';
        IF listed > 0 THEN
            RAISE EXCEPTION '作品 % 还有 % 条上架中的渠道，不能把对外版本清空', NEW.id, listed
                USING ERRCODE = 'restrict_violation';
        END IF;
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- ---------- 渠道守卫：上架要求「有对外版本」 ----------
--
-- 原来是「上架要求审核态 approved」。版本分离之后审核态描述的是**当前**版本，
-- 一件已发布作品在复审期间审核态是 pending 但对外内容仍在服务——用审核态拦
-- 会把发布与更新锁死。改判 published_revision_id IS NOT NULL 才是这件事的本意。

CREATE OR REPLACE FUNCTION trg_demo_channel_listed_requires_approved() RETURNS trigger AS $$
DECLARE
    pub bigint;
    st  text;
BEGIN
    IF NEW.visibility <> 'listed' THEN
        RETURN NEW;
    END IF;

    SELECT published_revision_id, moderation_state INTO pub, st FROM demos WHERE id = NEW.demo_id;
    -- 作品行还不存在时交给外键报错，不在这里抢答
    IF st IS NULL THEN
        RETURN NEW;
    END IF;
    IF pub IS NULL THEN
        RAISE EXCEPTION '作品 % 还没有对外版本（审核态 %），不能上架渠道 %', NEW.demo_id, st, NEW.channel
            USING ERRCODE = 'restrict_violation';
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- ---------- 时间线补 edit / rollback ----------
--
-- 设计 §7.2 的编辑时序写的是 `INSERT demo_timeline(kind=edit)`，而 0009 建表时
-- 只登记了 create / approve / reject / archive / restore / slug_change / merge，
-- 0012 补了 publish。编辑这一种一直没登记——因为更新功能一直没做。
--
-- 回滚（§7.3）复用旧清单产生新版本，它对作者来说是一个独立动作、值得单独出现在
-- 时间线上，所以一并登记。等真做回滚时不用再改一次 CHECK。

ALTER TABLE demo_timeline DROP CONSTRAINT ck_timeline_kind;
ALTER TABLE demo_timeline ADD CONSTRAINT ck_timeline_kind
    CHECK (kind IN ('create', 'approve', 'reject', 'archive', 'restore',
                    'slug_change', 'merge', 'publish', 'edit', 'rollback'));

COMMENT ON COLUMN demo_timeline.kind IS
    '对外可见的动作类型：create 新建 / approve 过审 / reject 驳回 / publish 发布到渠道 / edit 编辑产生新版本 / rollback 回滚到旧版本 / archive 下架 / restore 恢复 / slug_change 改地址 / merge 合并';
