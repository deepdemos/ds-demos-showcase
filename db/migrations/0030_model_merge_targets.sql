-- 0030：合并链的终点统一成一个视图，四条读路径都从它取。
--
-- 为什么要动：4c（#446）把 ResolveModelRefs 改成沿 merged_into_id 走到尽头，但
-- ListModels / ListRevisionModels / GetModelByID / GetModelBySlug 四条读路径仍是单跳
-- （`LEFT JOIN models t ON t.id = m.merged_into_id`）。A→B→C 两跳时，同一个旧名字
-- 在解析里得到 C、在展示里得到 B，而 B 自己也被合并掉了——两处指向不同模型，读者点到
-- 的是两个语义互相矛盾的链接。
--
-- 为什么是视图不是四条查询各抄一遍递归 CTE：不一致正是从「同一件事写两遍」来的。
-- 视图给这段递归一个名字，四条读路径只能 JOIN 它，口径就只有一份。
--
-- 为什么不物化：合并是低频写、读路径天天用；物化视图要配刷新，写错了得等刷新才
-- 看得见。现在这样每次读都按当前指针算，代价是查询里多一层 CTE。
--
-- 语义：沿指针走到 `status <> 'deprecated'`、`merged_into_id` 为 NULL、下一个
-- 节点已经在走过的路上（环）、或深度到 20 的节点。20 是运行手册第 10 条「实体合并
-- 链最大深度」的登记值。
--
-- 环要在递归里认出来，不能只靠深度上限：只有环长整除 20 时第 20 层才绕回起点，
-- 3 环（A→B→C→A）会停在 C——读者会看到一个指向环内成员的链接，点进去又是环。
-- 所以 walk 带一列 path，下一步要去的节点已在 path 里就停下；停下的那行由下面
-- 的 CASE 判成「归宿是自己」。
--
-- 与解析侧唯一一处故意的不同：环与悬空指针在这里让 `terminal_id` 等于被合并的
-- 那个模型自己，四条读路径再用 `t.id <> m.id` 把它滤成「没有归宿」——展示一个
-- 指回自己或已删除行的链接只会得到 404 或原地打转。解析侧不能这么做：它必须返回
-- 一个能落库的 id，否则作者手里的旧名字会重新变成一个待建的候选模型。
--
-- 悬空指针不会让整条链都变成自己：A→B 且 B 的归宿行被硬删时，A 的终点仍是存在
-- 的 B，只有 B 自己的终点是 B。
--
-- 超过 20 层的环认不出来：深度上限先停住，终点是第 20 层那个节点（真实存在，
-- 链接不会 404）。这是「20 层为界」的代价，写在这里免得读者以为环一定给空。
--
-- DISTINCT ON 是防守：正常情况下每个模型只有一行终点，但去掉深度上限的变异曾让
-- 同一模型吐出多个终点行；「视图一行/模型」的契约不能靠「当前 WHERE 恰好只命中
-- 一行」来保证。
CREATE VIEW model_merge_targets AS
WITH RECURSIVE walk AS (
    SELECT m.id AS model_id,
           m.id AS current_id,
           m.status,
           m.merged_into_id,
           0 AS depth,
           ARRAY[m.id] AS path
    FROM models m
    WHERE m.merged_into_id IS NOT NULL
    UNION ALL
    SELECT w.model_id,
           t.id,
           t.status,
           t.merged_into_id,
           w.depth + 1,
           w.path || t.id
    FROM walk w
    JOIN models t ON t.id = w.merged_into_id
    WHERE w.status = 'deprecated'
      AND w.merged_into_id IS NOT NULL
      AND w.depth < 20
      AND NOT (t.id = ANY (w.path))
)
SELECT DISTINCT ON (w.model_id)
       w.model_id,
       CASE
           -- 走到不再被合并的行：它就是归宿。
           WHEN w.status <> 'deprecated' THEN w.current_id
           WHEN w.merged_into_id IS NULL THEN w.current_id
           -- 20 层上限：停在最后一层真实存在的节点（与解析侧同一兜底）。
           WHEN w.depth >= 20 THEN w.current_id
           -- 悬空指针：停在最后那个存在的节点。
           WHEN NOT EXISTS (SELECT 1 FROM models t WHERE t.id = w.merged_into_id) THEN w.current_id
           -- 其余只可能是「下一个节点走过」：环没有归宿，给模型自己，读路径会滤掉。
           ELSE w.model_id
       END AS terminal_id
FROM walk w
ORDER BY w.model_id, w.depth DESC;
