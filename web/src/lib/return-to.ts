/**
 * 登录回执的落脚点（`?return_to=`）。
 *
 * 这个参数由前端自己拼、自己读，所以它同时是一个开放重定向入口：只要有人把
 * `?return_to=https://evil.example` 发出去，登录成功后就会把人送出站。这里只放行
 * 站内绝对路径——以单个 `/` 开头，且不是协议相对的 `//`。
 *
 * 校验放在这个模块里，是因为拼链接的地方有四个页面（登录、注册、验证邮箱、
 * 找回密码）。校验只写在读的那一头，写的那一头照样会把脏值传下去，链路一长
 * 就没人说得清值是从哪进来的。
 */
export function readReturnTo(raw: string | null | undefined): string {
  const value = raw ?? '';
  if (!value.startsWith('/') || value.startsWith('//')) return '/';
  return value;
}

/**
 * 拼到链接后面的 return_to 片段。没有可回的目标时返回空串——
 * 让「登录后回到 `/`」不要以 `?return_to=%2F` 的形式出现在地址栏里。
 */
export function returnToParam(raw: string | null | undefined, sep: '?' | '&' = '?'): string {
  const target = readReturnTo(raw);
  if (target === '/') return '';
  return `${sep}return_to=${encodeURIComponent(target)}`;
}

// 少数常去的落脚点给个人话名字。其余原样显示路径：地址栏里是什么，提示里就是
// 什么，比猜一个「上一页」之类的说法诚实。
const RETURN_LABELS: Record<string, string> = {
  '/submit': '投稿页',
  '/demos': '作品库',
  '/rankings': '排行榜',
  '/admin/reviews': '待审队列',
  '/admin/models': '模型导入',
};

export function returnToLabel(target: string): string {
  const known = RETURN_LABELS[target];
  if (known) return known;
  if (/^\/demos\/[^/]+\/edit$/.test(target)) return '这个作品的编辑页';
  return target;
}
