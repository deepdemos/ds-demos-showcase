import type { ReactElement } from 'react';

/**
 * 页面宽度档位。
 *
 * 页面不再各自写 max-w-*：宽度是站点级决定，视觉负责人换样式时不应该
 * 逐页改间距。档位的实际像素在 Page 组件里维护。
 */
export type PageWidth = 'auth' | 'form' | 'narrow' | 'default' | 'wide';

/** 页面归属的区。区决定套哪套外壳与哪组二级导航。 */
export type PageGroup = 'public' | 'me' | 'admin' | 'auth' | 'system';

/**
 * 访问门槛。
 *
 * 这是前端入口可见性与守卫的声明，不是权限判定——服务端仍然说了算。
 * permission.anyOf 里放的是会话接口摊平的权限键。
 */
export type RouteAccess =
  { kind: 'public' } | { kind: 'auth' } | { kind: 'permission'; anyOf: readonly string[] };

/** 页面是否已接真实数据。fixture 只允许出现在 DEV。 */
export type PageAvailability = 'ready' | 'fixture';

/** 页面出现在哪一组导航里。 */
export type NavArea = 'primary' | 'user' | 'me' | 'admin';

export interface NavEntry {
  area: NavArea;
  label: string;
  order: number;
  /** 管理区里的分组名，例如「待办」「内容」。 */
  group?: string;
  /** 只有持管理权限的人能看到（用于管理入口）。 */
  requiresAdmin?: boolean;
  /** 至少要有其中一个权限键才显示；空 = 不做权限过滤。 */
  permissions?: readonly string[];
}

export interface PageEntry {
  /** 稳定 id，用于路由元素表、导航与测试；不随文案变化。 */
  id: string;
  /** 浏览器地址；以 / 开头，404 用 *。 */
  path: string;
  /** 浏览器标签页标题；不含站名。 */
  title: string;
  /** 页面抬头的一句话说明；可空。 */
  description?: string;
  group: PageGroup;
  access: RouteAccess;
  width: PageWidth;
  availability: PageAvailability;
  /** 可选的导航登记；没登记的页面只能从别的页面进入。 */
  nav?: NavEntry;
}

/**
 * 页面清单：路由、标题、区、门槛、宽度的唯一事实源。
 *
 * 路由元素放在 route-elements.tsx，避免这份数据文件 import 页面组件；
 * 这样测试可以只读清单，不必挂载整棵树。
 *
 * 加页面的顺序：这里加一行 → route-elements.tsx 加元素 → 导航/重定向
 * （如果该页需要）→ 同 PR 补测试与文档。漏一步，routes.test.ts 会红。
 */
export const pages: readonly PageEntry[] = [
  {
    id: 'home',
    path: '/',
    title: '首页',
    description: '站点的入口与当前可去的地方。',
    group: 'public',
    access: { kind: 'public' },
    width: 'default',
    availability: 'ready',
  },
  {
    id: 'demos',
    path: '/demos',
    title: '作品库',
    description: '浏览、筛选与排序公开作品。',
    group: 'public',
    access: { kind: 'public' },
    width: 'wide',
    availability: 'ready',
    nav: { area: 'primary', label: '作品库', order: 10 },
  },
  {
    id: 'explore',
    path: '/explore',
    title: '探索',
    description: '热门、最新与模型目录的入口。',
    group: 'public',
    access: { kind: 'public' },
    width: 'wide',
    availability: 'ready',
    nav: { area: 'primary', label: '探索', order: 15 },
  },
  {
    id: 'demo-detail',
    path: '/demos/:slug',
    title: '作品详情',
    description: '作品内容、作者、模型声明与相关入口。',
    group: 'public',
    access: { kind: 'public' },
    width: 'default',
    availability: 'ready',
  },
  {
    id: 'demo-edit',
    path: '/demos/:slug/edit',
    title: '编辑作品',
    description: '修改作品信息并提交复审。',
    group: 'public',
    access: { kind: 'auth' },
    width: 'form',
    availability: 'ready',
  },
  {
    id: 'models',
    path: '/models',
    title: '模型目录',
    description: '站内模型档案与厂商列表。',
    group: 'public',
    access: { kind: 'public' },
    width: 'wide',
    availability: 'ready',
  },
  {
    id: 'model-detail',
    path: '/models/:slug',
    title: '模型',
    description: '模型档案、站内表现与相关作品。',
    group: 'public',
    access: { kind: 'public' },
    width: 'default',
    availability: 'ready',
  },
  {
    id: 'benchmarks',
    path: '/benchmarks',
    title: '第三方基准',
    description: '站内已收录的第三方基准：每张榜单独陈列，不跨榜比较。',
    group: 'public',
    access: { kind: 'public' },
    width: 'wide',
    availability: 'ready',
    // 不进一级导航（模型域 §5.6）：入口是模型详情页的第三区块。
  },
  {
    id: 'benchmark-sources',
    path: '/benchmarks/sources',
    title: '基准来源',
    description: '站外主流评测榜的速查表：测什么、怎么防污染、跑在什么环境、指标与成本、多久更新。',
    group: 'public',
    access: { kind: 'public' },
    width: 'wide',
    availability: 'ready',
    // 同样不进一级导航；入口是 /benchmarks 目录页（#568）。
  },
  {
    id: 'benchmark-overview',
    path: '/benchmarks/overview',
    title: '基准总览',
    description: '一行一个模型、一列一张榜：原始分与榜内名次，缺格是没测过。',
    group: 'public',
    access: { kind: 'public' },
    width: 'wide',
    availability: 'ready',
    // 同样不进一级导航；入口是 /benchmarks 目录页（#570）。
  },
  {
    id: 'benchmark-leaderboards',
    path: '/benchmarks/leaderboards',
    title: '排行榜',
    description: '每张榜一张卡片：名字、一句话说明、前几名预览；默认只看近期有数据的榜。',
    group: 'public',
    access: { kind: 'public' },
    width: 'wide',
    availability: 'ready',
    // 不进一级导航；入口是 /benchmarks 目录页。
  },
  {
    id: 'benchmark-detail',
    path: '/benchmarks/:slug',
    title: '基准',
    description: '单张基准的版本、指标与分数。',
    group: 'public',
    access: { kind: 'public' },
    width: 'default',
    availability: 'ready',
  },
  {
    id: 'rankings',
    path: '/rankings',
    title: '排行榜',
    description: '按不同口径与时间范围看作品。',
    group: 'public',
    access: { kind: 'public' },
    width: 'narrow',
    availability: 'ready',
    nav: { area: 'primary', label: '排行榜', order: 20 },
  },
  {
    id: 'author',
    path: '/u/:username',
    title: '作者',
    description: '作者资料与公开作品。',
    group: 'public',
    access: { kind: 'public' },
    width: 'narrow',
    availability: 'ready',
  },
  {
    id: 'classification',
    path: '/classifications/:dimension/:slug',
    title: '分类',
    description: '一个分类取值下的作品与相邻取值。',
    group: 'public',
    access: { kind: 'public' },
    width: 'wide',
    availability: 'ready',
  },
  {
    id: 'guidelines',
    path: '/community-guidelines',
    title: '社区规范',
    description: '站内内容与行为的规则。',
    group: 'public',
    access: { kind: 'public' },
    width: 'narrow',
    availability: 'ready',
  },
  {
    id: 'about',
    path: '/about',
    title: '关于',
    description: '这个站点是什么、数据从哪来。',
    group: 'public',
    access: { kind: 'public' },
    width: 'narrow',
    availability: 'ready',
  },
  {
    id: 'search',
    path: '/search',
    title: '搜索',
    description: '按标题或简介搜索公开作品。',
    group: 'public',
    access: { kind: 'public' },
    width: 'wide',
    availability: 'ready',
  },
  {
    id: 'submit',
    path: '/submit',
    title: '投稿',
    description: '提交一件新作品。',
    group: 'public',
    access: { kind: 'auth' },
    width: 'form',
    availability: 'ready',
    nav: { area: 'primary', label: '投稿', order: 30 },
  },
  {
    id: 'collections',
    path: '/me/collections',
    title: '收藏夹',
    description: '自己的收藏夹列表。',
    group: 'me',
    access: { kind: 'auth' },
    width: 'narrow',
    availability: 'ready',
    nav: { area: 'user', label: '收藏夹', order: 20 },
  },
  {
    id: 'collection-detail',
    path: '/me/collections/:id',
    title: '收藏夹详情',
    description: '一个收藏夹里的作品与设置。',
    group: 'me',
    access: { kind: 'auth' },
    width: 'narrow',
    availability: 'ready',
  },
  {
    id: 'notifications',
    path: '/me/notifications',
    title: '通知',
    description: '审核结果、评分与站内消息。',
    group: 'me',
    access: { kind: 'auth' },
    width: 'narrow',
    availability: 'ready',
    nav: { area: 'user', label: '通知', order: 40 },
  },
  {
    id: 'my-demos',
    path: '/me/demos',
    title: '我的作品',
    description: '自己作品的状态、预览与编辑入口。',
    group: 'me',
    access: { kind: 'auth' },
    width: 'wide',
    availability: 'ready',
    nav: { area: 'user', label: '我的作品', order: 10 },
  },
  {
    id: 'history',
    path: '/me/history',
    title: '浏览历史',
    description: '本机与账号两条来源的浏览记录。',
    group: 'me',
    // HIST-001：本机历史不要求登录。
    access: { kind: 'public' },
    width: 'narrow',
    availability: 'ready',
    nav: { area: 'user', label: '浏览历史', order: 30 },
  },
  {
    id: 'settings',
    path: '/me/settings',
    title: '设置',
    description: '账号资料、偏好与内容管理入口。',
    group: 'me',
    access: { kind: 'auth' },
    width: 'narrow',
    availability: 'ready',
    nav: { area: 'user', label: '设置', order: 50 },
  },
  {
    id: 'login',
    path: '/login',
    title: '登录',
    description: '登录后回到原来的页面。',
    group: 'auth',
    access: { kind: 'public' },
    width: 'auth',
    availability: 'ready',
  },
  {
    id: 'register',
    path: '/register',
    title: '注册',
    description: '创建账号并验证邮箱。',
    group: 'auth',
    access: { kind: 'public' },
    width: 'auth',
    availability: 'ready',
  },
  {
    id: 'verify-email',
    path: '/verify-email',
    title: '验证邮箱',
    description: '用邮件验证码激活账号。',
    group: 'auth',
    access: { kind: 'public' },
    width: 'auth',
    availability: 'ready',
  },
  {
    id: 'password-reset',
    path: '/password-reset',
    title: '重置密码',
    description: '用邮件验证码设置新密码。',
    group: 'auth',
    access: { kind: 'public' },
    width: 'auth',
    availability: 'ready',
  },
  {
    id: 'admin-inbox',
    path: '/admin/inbox',
    title: '收件箱',
    description: '待处理的审核、提案与申诉。',
    group: 'admin',
    access: { kind: 'auth' },
    width: 'wide',
    availability: 'ready',
    nav: {
      area: 'admin',
      label: '收件箱',
      order: 10,
      group: '待办',
      requiresAdmin: true,
      permissions: ['demo.review', 'proposal.review'],
    },
  },
  {
    id: 'admin-reviews',
    path: '/admin/reviews',
    title: '作品审核',
    description: '待审作品与通过后的发布。',
    group: 'admin',
    access: { kind: 'auth' },
    width: 'wide',
    availability: 'ready',
    nav: {
      area: 'admin',
      label: '作品审核',
      order: 20,
      group: '待办',
      requiresAdmin: true,
      permissions: ['demo.review', 'demo.publish'],
    },
  },
  {
    id: 'admin-proposal',
    path: '/admin/proposals/:id',
    title: '提案详情',
    description: '一条提案的内容与裁决。',
    group: 'admin',
    access: { kind: 'auth' },
    width: 'wide',
    availability: 'ready',
  },
  {
    id: 'admin-proposals',
    path: '/admin/proposals',
    title: '提案',
    description: '用户实体提案列表与裁决入口。',
    group: 'admin',
    access: { kind: 'auth' },
    width: 'wide',
    availability: 'ready',
    nav: {
      area: 'admin',
      label: '提案',
      order: 30,
      group: '待办',
      permissions: ['proposal.review'],
    },
  },
  {
    id: 'admin-appeals',
    path: '/admin/appeals',
    title: '申诉',
    description: '被移除作品的申诉裁决。',
    group: 'admin',
    access: { kind: 'auth' },
    width: 'wide',
    availability: 'ready',
    nav: {
      area: 'admin',
      label: '申诉',
      order: 30,
      group: '待办',
      requiresAdmin: true,
      permissions: ['content.erase', 'proposal.review'],
    },
  },
  {
    id: 'admin-models',
    path: '/admin/models',
    title: '模型导入',
    description: '同步上游目录并导入模型字段。',
    group: 'admin',
    access: { kind: 'auth' },
    width: 'wide',
    availability: 'ready',
    nav: {
      area: 'admin',
      label: '模型导入',
      order: 10,
      group: '内容',
      requiresAdmin: true,
      permissions: ['model.import'],
    },
  },
  {
    id: 'not-found',
    path: '*',
    title: '页面不存在',
    description: '地址不对，给出两条回去的路。',
    group: 'system',
    access: { kind: 'public' },
    width: 'narrow',
    availability: 'ready',
  },
];

/** 旧地址 → 新地址。跳转时保留查询串；参数在 RedirectPage 里替换。 */
export interface RedirectEntry {
  from: string;
  to: string;
}

/**
 * 兼容入口。
 *
 * 只列已经有落点的地址：`/settings/*` 等页面还没做，先不登记一个会 404 的跳转。
 * `/admin` 的落点是收件箱；`/me` 的落点是「我的作品」。
 */
export const redirects: readonly RedirectEntry[] = [
  { from: '/me', to: '/me/demos' },
  { from: '/my/demos', to: '/me/demos' },
  { from: '/notifications', to: '/me/notifications' },
  { from: '/collections', to: '/me/collections' },
  { from: '/collections/:id', to: '/me/collections/:id' },
  { from: '/history', to: '/me/history' },
  { from: '/admin', to: '/admin/inbox' },
];

export function findPage(id: string): PageEntry {
  const page = pages.find((entry) => entry.id === id);
  if (!page) throw new Error('未登记的路由 id：' + id);
  return page;
}

/** 把清单里的绝对路径转成嵌套路由的相对路径。 */
export function childPath(path: string): string {
  if (path === '/') return '';
  return path.replace(/^\//, '');
}

/** 路由元素表必须覆盖清单的每一个 id；测试与 router 共用这条规则。 */
export type RouteElementMap = Record<string, ReactElement>;
