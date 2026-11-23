/**
 * 陈列档位的唯一词表（#568）。
 *
 * 档位说的是「这张榜我们怎么陈列」，不是「这张榜好不好」：
 * - `table` 分数进站，页面展示原始分与名次；
 * - `cite` 只给带署名的引用与官方外链，分数不入库；
 * - `link` 只有名称、简介与链接；
 * - `none` 只在来源表里留档（停更 / 授权不足），库里的 `display_tier` 没有这一档。
 *
 * 文案只写在这里：目录页、详情页、来源页读同一份。目录页与详情页原先各写了一遍，
 * 新增来源页时会变成第三份 —— 三处各改一次必然改岔，所以收敛到一处。
 * 门禁 `scripts/check-single-source.mjs` 的 `display-tier-label` 规则会拦住新副本。
 */
export type DisplayTier = 'table' | 'cite' | 'link' | 'none';

export const tierLabel: Record<DisplayTier, string> = {
  table: '有分数',
  cite: '仅引用',
  link: '仅链接',
  none: '不收录',
};
