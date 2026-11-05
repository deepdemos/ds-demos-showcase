-- 内容寻址存储（零拷贝）：blobs 存「一份字节」，demo_revision_files 存「某个版本有哪些路径」。
--
-- 设计意图（Demo域设计 §7.2）：版本是不可变的，但**文件是可共享的**。作者第二版只改了
-- 一个 CSS 时，其余文件不该在磁盘上再存一份。两个版本的清单里各有一行指向同一个
-- `blobs.sha256`，磁盘上只有一份字节。

-- name: GetBlob :one
-- 写盘前先查：命中就完全不碰磁盘（零拷贝的全部收益都来自这一步）。
SELECT sha256, size, storage_key, backend
FROM blobs
WHERE sha256 = $1;

-- name: InsertBlob :exec
-- 已存在时只更新 last_ref_at：它记录「最后一次被引用的时间」，
-- 将来 GC 扫「长期没人引用」的对象时要用（现在还没有 GC，见 docs/已知松点.md）。
--
-- 用 ON CONFLICT 而不是「先查再插」：并发投稿同一个文件是正常情况
-- （同一份 index.html 被两个人上传），先查后插有 TOCTOU 窗口。
INSERT INTO blobs (sha256, size, mime, storage_key, backend)
VALUES ($1, $2, $3, $4, 'local')
ON CONFLICT (sha256) DO UPDATE
SET last_ref_at = now();

-- name: InsertRevisionFile :exec
-- 清单行：这个版本在这个路径上放的是哪份字节。
-- 重复插同一个 (revision_id, path) 不报错——重试上传时不该因为已经写过而失败。
INSERT INTO demo_revision_files (revision_id, path, blob_hash, size, mime)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT ON CONSTRAINT uq_manifest_path DO NOTHING;

-- name: GetRevisionFile :one
-- 读路径的主查询：给定「作品的 slug + 版本号 + 相对路径」，返回字节在存储里的 key。
--
-- 按 slug + rev_no 而不是 revision_id 查，是为了**不改动**预览与下载现有的行结构：
-- 那两处已经从作品行上取到了版本号，再让它们多带一个 revision_id 只是把复杂度
-- 挪个地方。
SELECT b.storage_key, b.size, f.mime
FROM demo_revision_files f
JOIN demo_revisions r ON r.id = f.revision_id
JOIN demos d          ON d.id = r.demo_id
JOIN blobs b          ON b.sha256 = f.blob_hash
WHERE d.slug = $1 AND r.rev_no = $2 AND f.path = $3;

-- name: CountRevisionFiles :one
-- 「这个版本有清单吗」——没有清单的版本是迁移前写入的旧数据，
-- 读路径要回退到 `demos/{slug}/r{n}/` 目录布局。
SELECT count(*) FROM demo_revision_files WHERE revision_id = $1;
