-- 0031：端点上报落地（#445 的 4d）——给 provider_id 补一条长度闸。
--
-- 0028 只给 authority（1..128）与 external_id（1..256）设了长度 CHECK，provider_id 没有：
-- 那时它只装目录镜像的 provider 键（实测最长 24），宽度由上游决定，不由调用方决定。
--
-- 4d 起它装的是**规范化后的 base_url**（端点上报的出处），这是外部输入直接决定的宽度。
-- 不封顶等于让调用方决定索引键的长度：一行索引条目可以长到几百字节，几千行之后
-- 就是一次没人预期的膨胀。512 的依据：目录侧实测最长 24；真实 base_url 绝大多数在
-- 64 字符内，512 留足余量（含路径前缀 /v1、/openai/v1 这类），又不至于失控。
--
-- 0 也允许：目录侧存在 provider_id 为空串的行（只有 canonical 的条目），这条闸不是
-- 「必须有出处」，而是「出处不能无限长」。
--
-- 服务端在写之前先挡成 400（`maxEndpointLen`），这条 CHECK 是最后一道——
-- 与 0028 那两条同样的分工：对外是 400，对内是约束。
-- 三条宽度（authority / external_id / provider_id）与 `ck_model_checkpoints_label` 的
-- 1..64 一起登记进 `docs/运行手册-可变常量.md` §7.5 第 46–49 条（2026-11-17，v1.8），
-- 这正是 0028 / 0029 文件头留的那句话「等端点写入方真正落地时一起登记」。
--
-- 存量数据零影响：库里 provider_id 最长 24，全部满足。

ALTER TABLE model_offerings ADD CONSTRAINT ck_model_offerings_provider_id
    CHECK (length(provider_id) BETWEEN 0 AND 512);
