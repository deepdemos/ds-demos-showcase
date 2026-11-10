-- 0026：让「外链」类型真的能投（`demo_type_formats` 加 url）
--
-- 在此之前 `link` 这一行存在、清单是空的——空清单的含义是「界面上不可选」。
-- 现在后端支持「不上传文件、只交一个外部地址」，清单才该有内容。
--
-- 存地址的列早就在：`demo_revisions.external_url`，长度上限 2000 由
-- `ck_rev_external_url_len` 保证。这一批没有新列、没有新表。

INSERT INTO demo_type_formats (demo_type_code, format, sort) VALUES
    ('link', 'url', 10)
ON CONFLICT (demo_type_code, format) DO NOTHING;
