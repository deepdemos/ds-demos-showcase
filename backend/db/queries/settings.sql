-- name: GetSiteSettingNumber :one
-- 读一个 number 类型的站点参数：有覆盖读覆盖，没有读注册表里的默认值。
--
-- 返回 text 而不是 numeric：调用方要的是「30」这样的整数天，而 numeric
-- 扫进 Go 会经过 pgtype.Numeric，多一层转换还得处理 NaN。这里统一在 Go 里解析。
--
-- 缺键直接 ErrNoRows（不返回 0）：键名拼错的后果是「恢复窗口变成 0 天」，
-- 那会让每一次删除都立刻不可恢复——必须在开发期就炸，不能静默退化。
SELECT COALESCE(s.value, k.default_value)::text AS raw
FROM setting_keys k
LEFT JOIN site_settings s ON s.key = k.key
WHERE k.key = @key::text
  AND k.scope = 'site'
  AND k.value_type = 'number'
  AND k.is_active;
