import type { Me } from '@/api';
import { pages, type PageEntry } from './routes';

/**
 * 管理区入口的权限键集合。
 *
 * 这里只做入口可见性：真正的判定在服务端。列的是当前已经登记过的
 * 管理类权限键；新增权限键时同步这里，否则入口可能不显示。
 */
export const ADMIN_PERMISSIONS = [
  'demo.review',
  'demo.publish',
  'demo.curate',
  'demo.featured',
  'demo.delete',
  'demo.rebuild',
  'content.erase',
  'proposal.review',
  'model.import',
  'user.read',
  'user.status',
  'user.role',
  'audit.read',
  'setting.write',
  'classification.edit',
  'ops.run',
] as const;

export interface NavItem {
  id: string;
  label: string;
  to: string;
  /** 通知这类带数字的入口。 */
  badge?: number;
  /** 管理区里的分组名；一级与个人导航不用。 */
  group?: string;
}

function canSee(me: Me | null, page: PageEntry): boolean {
  if (page.availability !== 'ready') return false;
  const adminRole = me?.role === 'admin';
  if (page.nav?.permissions?.length && !adminRole && !hasAnyPermission(me, page.nav.permissions)) {
    return false;
  }
  if (page.nav?.requiresAdmin) return hasAdminAccess(me);
  return true;
}

export function hasAnyPermission(me: Me | null, keys: readonly string[]): boolean {
  return me !== null && keys.some((key) => me.permissions.includes(key));
}

/**
 * 管理区是否可达。
 *
 * 角色兜底是为了「权限键摊平失败但角色明明是管理员」的情况：服务端仍是最终
 * 判定，前端不能因为会话字段暂时缺项就把人挡在门外。
 */
export function hasAdminAccess(me: Me | null): boolean {
  if (me === null) return false;
  if (me.role === 'admin' || me.role === 'moderator') return true;
  return hasAnyPermission(me, ADMIN_PERMISSIONS);
}

function toItem(page: PageEntry): NavItem {
  return {
    id: page.id,
    label: page.nav?.label ?? page.title,
    to: page.path,
    ...(page.nav?.group ? { group: page.nav.group } : {}),
  };
}

function sorted(area: 'primary' | 'user' | 'admin'): PageEntry[] {
  return pages
    .filter((page) => page.nav?.area === area)
    .slice()
    .sort((a, b) => (a.nav?.order ?? 0) - (b.nav?.order ?? 0));
}

/** 一级入口：作品库 / 排行榜 / 投稿；后续批次会加探索与讨论区。 */
export function primaryNav(me: Me | null): NavItem[] {
  return sorted('primary')
    .filter((page) => canSee(me, page))
    .map(toItem);
}

/** 个人区二级导航：我的作品 / 收藏夹 / 浏览历史 / 通知。 */
export function meNav(me: Me | null): NavItem[] {
  if (me === null) return [];
  return sorted('user')
    .filter((page) => canSee(me, page))
    .map(toItem);
}

/** 用户菜单：个人入口 + 管理入口；管理入口按权限键决定。 */
export function userNav(me: Me | null): NavItem[] {
  if (me === null) return [];
  const items = meNav(me);
  if (hasAdminAccess(me)) {
    items.push({ id: 'admin', label: '管理', to: '/admin/inbox' });
  }
  return items;
}

/** 管理区二级导航的分组顺序；AdminLayout 与测试共用。 */
export const ADMIN_NAV_GROUPS = ['待办', '内容', '治理'] as const;

function adminGroupIndex(group: string | undefined): number {
  return ADMIN_NAV_GROUPS.indexOf((group ?? '') as (typeof ADMIN_NAV_GROUPS)[number]);
}

/** 管理区二级导航；先按分组、再按 order，交给 AdminLayout 渲染。 */
export function adminNav(me: Me | null): NavItem[] {
  if (!hasAdminAccess(me)) return [];
  return sorted('admin')
    .filter((page) => canSee(me, page))
    .sort((a, b) => {
      const ga = adminGroupIndex(a.nav?.group);
      const gb = adminGroupIndex(b.nav?.group);
      if (ga !== gb) return ga - gb;
      return (a.nav?.order ?? 0) - (b.nav?.order ?? 0);
    })
    .map(toItem);
}
