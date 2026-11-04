-- 给「已发布但没有渠道行」的作品补一条 deep/listed 渠道行。
--
-- 为什么需要这条：列表的可见性谓词从「只看 demos」改成「demos + 至少一条
-- visibility='listed' 的渠道行」，这样发布时选的 unlisted / hidden 才真的生效。
-- 但在这条谓词上线之前发布的作品根本没有渠道行——不补的话，它们会在上线瞬间
-- 从作品库里消失，看起来像数据丢了。
--
-- 只补 approved + published + 未删未擦的行：其余行本来就不该出现在列表里，
-- 给它们渠道行反而会让下一次判定的语义变得含糊。
--
-- 幂等：ON CONFLICT DO NOTHING，重复执行不会覆盖管理员已经改过的 visibility。

INSERT INTO demo_channels (demo_id, channel, visibility, lang, published_at)
-- published_at 留 NULL：这些作品的真实发布时间没有留存，写 now() 是编造，
-- 写 d.created_at 又会撞 ck_channel_time（published_at 必须 >= 渠道行自己的 created_at）。
-- NULL 的语义就是「不知道」，列表也不读这一列。
SELECT d.id, 'deep', 'listed', 'zh', NULL::timestamptz
FROM demos d
WHERE d.lifecycle_state = 'published'
  AND d.moderation_state = 'approved'
  AND d.deleted_at IS NULL
  AND d.erased_at IS NULL
  AND NOT EXISTS (
      SELECT 1 FROM demo_channels c WHERE c.demo_id = d.id
  )
ON CONFLICT (demo_id, channel) DO NOTHING;
