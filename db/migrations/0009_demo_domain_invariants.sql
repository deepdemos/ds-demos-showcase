-- 0009：把 demo 域里「数据自洽且出错时静默」的三条不变量落成触发器。
--
-- 对应 `docs/Demo域设计.md` §9：
--   #1 revision 一旦创建不得 UPDATE（除 deleted_at 与一次擦除）
--   #2 `current_revision_id` 必须指向**本 demo 的** revision
--   #10 `visibility='listed'` 要求父作品 `moderation_state='approved'`
--
-- 另外两条不变量没做，理由写在 `docs/已知松点.md`：
--   #3「每个 demo 至少一条 revision」——投稿事务里是两阶段的（先 INSERT demos
--      再 INSERT demo_revisions），非延迟约束会让第一步就失败；做成
--      DEFERRABLE INITIALLY DEFERRED 又把「建 demo 必须在一个事务里」变成硬约束，
--      将来加「先存草稿、稍后补内容」就会撞墙。
--   #8 / #11 / #13 是查询口径与操作纪律，塞进 schema 只会把策略写死。

-- ---------- #1 revision 不可变 ----------
-- 用整行的 jsonb 相减来比，而不是列清单：列清单要跟着 schema 一直维护，
-- 忘记更新就会让新列悄悄变成可改的。
CREATE FUNCTION trg_demo_revision_immutable() RETURNS trigger AS $$
BEGIN
    IF OLD.erased_at IS NOT NULL THEN
        RAISE EXCEPTION 'demo_revisions 已擦除，不可再修改（id=%）', OLD.id
            USING ERRCODE = 'restrict_violation';
    END IF;

    -- 擦除那一次允许改内容列（按 §9 #7 置 NULL 并写 erased_at）
    IF NEW.erased_at IS NOT NULL AND OLD.erased_at IS NULL THEN
        RETURN NEW;
    END IF;

    IF (to_jsonb(NEW) - 'deleted_at') IS DISTINCT FROM (to_jsonb(OLD) - 'deleted_at') THEN
        RAISE EXCEPTION 'demo_revisions 一旦创建即不可变，只允许改 deleted_at（id=%）', OLD.id
            USING ERRCODE = 'restrict_violation';
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_demo_revisions_immutable
    BEFORE UPDATE ON demo_revisions
    FOR EACH ROW EXECUTE FUNCTION trg_demo_revision_immutable();

-- ---------- #2 指针必须指向自己的版本；#10 上架的渠道要求作品已通过审核 ----------
CREATE FUNCTION trg_demos_guard() RETURNS trigger AS $$
DECLARE
    owner bigint;
    listed int;
BEGIN
    -- #2：current_revision_id 的 demo_id 必须等于本行 id。
    -- 没有这条时，A 的指针可以指到 B 的版本上，而所有读路径都会照读。
    IF NEW.current_revision_id IS NOT NULL THEN
        SELECT demo_id INTO owner FROM demo_revisions WHERE id = NEW.current_revision_id;
        IF owner IS DISTINCT FROM NEW.id THEN
            RAISE EXCEPTION 'current_revision_id=% 属于作品 %，不属于作品 %',
                NEW.current_revision_id, owner, NEW.id
                USING ERRCODE = 'restrict_violation';
        END IF;
    END IF;

    -- #10 的反向：作品从 approved 退回时，不能留下上架中的渠道。
    -- 下架的正路是把 lifecycle_state 设成 removed（渠道随之 hidden），
    -- 而不是把 moderation_state 改回去。这里拦下来是为了不让它静默发生。
    IF TG_OP = 'UPDATE'
       AND OLD.moderation_state = 'approved'
       AND NEW.moderation_state <> 'approved' THEN
        SELECT count(*) INTO listed
        FROM demo_channels WHERE demo_id = NEW.id AND visibility = 'listed';
        IF listed > 0 THEN
            RAISE EXCEPTION '作品 % 还有 % 条上架中的渠道，先把它们的 visibility 改掉再退回审核状态', NEW.id, listed
                USING ERRCODE = 'restrict_violation';
        END IF;
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_demos_guard
    BEFORE INSERT OR UPDATE ON demos
    FOR EACH ROW EXECUTE FUNCTION trg_demos_guard();

-- ---------- #10 的正向 ----------
CREATE FUNCTION trg_demo_channel_listed_requires_approved() RETURNS trigger AS $$
DECLARE
    st text;
BEGIN
    IF NEW.visibility <> 'listed' THEN
        RETURN NEW;
    END IF;

    SELECT moderation_state INTO st FROM demos WHERE id = NEW.demo_id;
    -- 作品行还不存在时交给外键报错，不在这里抢答
    IF st IS NULL THEN
        RETURN NEW;
    END IF;
    IF st <> 'approved' THEN
        RAISE EXCEPTION '作品 % 的审核状态是 %，不能上架渠道 %', NEW.demo_id, st, NEW.channel
            USING ERRCODE = 'restrict_violation';
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_demo_channels_listed_requires_approved
    BEFORE INSERT OR UPDATE ON demo_channels
    FOR EACH ROW EXECUTE FUNCTION trg_demo_channel_listed_requires_approved();
