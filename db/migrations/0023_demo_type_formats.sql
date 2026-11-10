-- 0023：作品类型与上传形态的绑定（demo_type_formats）
--
-- # 为什么要有这张表
--
-- 在此之前 `demo_types` 只是一个标签表：作者选「压缩包作品」却上传一个 .html
-- 也能通过，因为投稿校验只看文件，不看作者声明的类型。类型与形态是两套独立
-- 概念，却没有任何一处把它们对上——「能投」与「投对了」是两件事。
--
-- 这张表把「这个类型收哪些形态」变成数据：加一种格式 = 加一行，不改代码。
-- 形态词汇本身仍由 backend/internal/domain/demo/shapes.go 定义（单一出处），
-- 这里只存字符串，取值合法性由 Go 侧校验并在集成测试里对齐。
--
-- # 格式记法
--
--   single:<kind>  单文件作品，kind 是 `demo_revisions.single_file` 的取值
--   zip:<entry>    zip 包，entry 说明它是哪种包（html 站点 / godot 导出）
--   url            没有上传文件，作者给一个外部地址
--
-- 现在只登记流水线真的支持的形态：`image` 与 `link` 还没有上传路径，
-- 因此它们的允许清单是空的——空清单的含义是「在界面上不可选」，不是「任意」。

CREATE TABLE demo_type_formats (
    demo_type_code text        NOT NULL REFERENCES demo_types (code) ON DELETE CASCADE,
    format         text        NOT NULL,
    sort           int         NOT NULL DEFAULT 0,
    created_at     timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (demo_type_code, format),
    CONSTRAINT ck_demo_type_formats_format
        CHECK (format = 'url' OR format ~ '^(single|zip):[a-z][a-z0-9_]*$')
);

COMMENT ON TABLE demo_type_formats IS '作品类型允许的上传形态；加格式是加数据不是改代码';

-- Godot 游戏单列一个类型：作者一眼知道该选哪个，审核也知道该看什么。
-- 它与 `zip` 的区别不在文件容器，而在包内结构（index.html + .wasm + .pck）。
INSERT INTO demo_types (code, name, sort) VALUES
    ('godot', 'Godot 游戏', 35)
ON CONFLICT (code) DO NOTHING;

INSERT INTO demo_type_formats (demo_type_code, format, sort) VALUES
    ('web',   'single:html', 10),
    ('web',   'single:svg',  20),
    ('web',   'zip:html',    30),
    ('zip',   'zip:html',    10),
    ('godot', 'zip:godot',   10)
ON CONFLICT DO NOTHING;