-- 新增「三维模型」类型，并把图片类型能收的格式登记上。
--
-- 两者都是**单文件**形态：一个 .glb 就是全部内容（对比 Godot 要一个 zip）。
-- 图片此前只有一个空清单——类型行在、没有上传路径，界面上只能不可选；
-- 现在把清单填上，作者才真的能投。
--
-- 压缩过的 glb（KHR_draco_mesh_compression / KTX2 / meshopt）预览需要解码器，
-- 本次只内置了 model-viewer 本体，没带解码器：见 docs/已知松点.md 第 60 条。

INSERT INTO demo_types (code, name, sort, status) VALUES
    ('model', '三维模型', 50, 'active')
ON CONFLICT (code) DO UPDATE SET name = EXCLUDED.name, sort = EXCLUDED.sort;

INSERT INTO demo_type_formats (demo_type_code, format, sort) VALUES
    ('model', 'single:glb', 10),
    ('image', 'single:png', 10),
    ('image', 'single:jpg', 20),
    ('image', 'single:webp', 30)
ON CONFLICT (demo_type_code, format) DO UPDATE SET sort = EXCLUDED.sort;