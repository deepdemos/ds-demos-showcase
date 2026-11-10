-- 单文件形态从两种扩到六种：新增 glb 与 png / jpg / webp。
--
-- 这条约束是「形态词汇」的另一半：`shapes.go` 决定代码认识哪些，这条 CHECK 决定库里
-- 允许哪些。两边必须一起改——只改代码，投稿会在落库那一刻被 23514 顶回来，而错误
-- 信息是「落库失败」，看起来像存储或事务出了问题。
--
-- 仓库没有「改基线」的先例：约束在账号/作品域里属于基线，按规则只加迁移。

ALTER TABLE demo_revisions DROP CONSTRAINT IF EXISTS ck_rev_single_file;

ALTER TABLE demo_revisions
    ADD CONSTRAINT ck_rev_single_file
    CHECK (single_file IS NULL OR single_file IN ('html', 'svg', 'glb', 'png', 'jpg', 'webp'));