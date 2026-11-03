-- 0006：补上 demos ↔ demo_revisions 之间缺失的两条外键。
--
-- 这两条边一直是有意留白的：demos.current_revision_id 指向 demo_revisions，
-- demo_revisions.demo_id 又指回 demos，互为先后，建表顺序无法同时满足。
-- 80-late-fk.sql 的头部也拿这一对当例子。但「集中添加外键」那一轮漏掉了它们，
-- 于是这两列至今没有约束——删作品不会带走版本行，版本行可以指向不存在的作品。
--
-- 两条边的动作选得不一样，各有理由：
--
--   1. demo_revisions.demo_id → demos：ON DELETE CASCADE。
--      版本属于作品，作品没了版本就该一起没。
--
--   2. demos.current_revision_id → demo_revisions：ON DELETE SET NULL。
--      指针不是拥有者，所以不能 CASCADE（那会把作品删掉）；
--      也不能 RESTRICT（删作品时版本先被 CASCADE 删掉，会撞在指针上）。
--      SET NULL 让指针在版本消失时自动作废。
--
-- 这两条放在同一次 DELETE 上会互相踩：删 demos 时它的 demo_revisions 被
-- CASCADE 删除，而被删的那些行正是 demos.current_revision_id 指向的目标。
-- 是否需要在 current_revision_id 上加 DEFERRABLE 取决于外键检查的时机——
-- 这一点我**没有靠推理**，在 PostgreSQL 18.6（本机 5433）上实测过：
--
--     CREATE TABLE z_demo(id bigint primary key, cur bigint);
--     CREATE TABLE z_rev (id bigint primary key, demo_id bigint not null);
--     ALTER TABLE z_rev  ADD CONSTRAINT z_fk_rev FOREIGN KEY (demo_id) REFERENCES z_demo(id) ON DELETE CASCADE;
--     ALTER TABLE z_demo ADD CONSTRAINT z_fk_cur FOREIGN KEY (cur) REFERENCES z_rev(id) ON DELETE SET NULL;
--     -- 一个作品三个版本，指针指向中间那个
--     DELETE FROM z_demo WHERE id=1;
--     → DELETE 1；z_demo 剩 0 行、z_rev 剩 0 行
--
-- 也就是说**不加 DEFERRABLE 也能过**：SET NULL 的动作先于外键检查完成。
-- 所以这里就是普通的、立即检查的外键，不额外放松约束。

ALTER TABLE demo_revisions
    ADD CONSTRAINT fk_demo_revisions_demo_id76
    FOREIGN KEY (demo_id) REFERENCES demos (id) ON DELETE CASCADE;

ALTER TABLE demos
    ADD CONSTRAINT fk_demos_current_revision77
    FOREIGN KEY (current_revision_id) REFERENCES demo_revisions (id)
    ON DELETE SET NULL;

-- 外键不会自动给引用列建索引。demo_revisions 上已有 uq_demo_revisions_no
-- (demo_id, rev_no) 以 demo_id 为前导列，删作品时的级联扫描走它即可；
-- demos.current_revision_id 没有「按当前版本反查作品」的查询，不另建索引。
