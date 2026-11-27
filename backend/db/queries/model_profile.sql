-- 模型档案的批量读取。
--
-- 为什么单独一个文件而不是塞进 models.sql：那个文件 41 条查询、893 行，是所有模型
-- 写路径的共同瓶颈（并行开发时天天撞车）。新查询按域拆开，sqlc 扫目录即可。
--
-- 为什么是「全量档案」而不是按 model_id 列表传参：档案行数只会比模型少（一张
-- PK 为 model_id 的子表），全量取一次再在 Go 侧按 id 分组，比把几十个 id 拼进
-- ANY(...) 更省，也和 ListModelAliases 的做法一致。

-- name: ListModelSpecs :many
-- 全部档案，按 model_id 定序。
--
-- 列集与 `GetModelSpecs` **逐字一致**（同名列、同顺序），这样两处的行类型可以互相
-- 强转（Go 允许忽略结构体标签的同形转换），映射函数只写一份。漏改一处就是
-- 「详情有档案、列表没有」这种只在某一个入口才出现的偏差。
SELECT model_id,
       context_window,
       max_output,
       param_total_b,
       param_active_b,
       param_kind,
       param_note,
       release_date,
       knowledge_cutoff,
       open_weights,
       cost_in,
       cost_out,
       license,
       modalities_in,
       modalities_out,
       source,
       fetched_at,
       note
FROM model_specs
ORDER BY model_id;
