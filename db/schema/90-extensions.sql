-- 90-extensions.sql —— 需要 contrib 扩展的对象（顺序必须在所有表之后）
-- 这些语句在「装不了扩展」的环境（如 PGlite 内置构建）会整块跳过，
-- scripts/apply-schema.mjs 会先探测 pg_available_extensions 再决定是否执行本文件。
-- 官方 PostgreSQL 镜像自带 contrib，生产与本地真库都能装。

-- 提示词模糊检索（实体域 §5.6）
CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE INDEX ix_prompts_content_trgm ON prompts USING gin (content gin_trgm_ops);
