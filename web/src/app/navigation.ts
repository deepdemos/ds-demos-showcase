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
}

function canSee(me: Me | null, page: PageEntry): boolean {
  if (page.availability !== 'ready') return false;
  if (page.nav?.requiresAdmin) return hasAdminAccess(me);
  return true;
}

export function hasAnyPermission(me: Me | null, keys: readonly string[]): boolean {
  return me !== null && keys.some((key) => me.permissions.includes(key));
}

export function hasAdminAccess(me: Me | null): boolean {
  return hasAnyPermission(me, ADMIN_PERMISSIONS);
}

function toItem(page: PageEntry): NavItem {
  return { id: page.id, label: page.nav?.label ?? page.title, to: page.path };
}

function sorted(area: 'primary' | 'user'): PageEntry[] {
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

/** 用户菜单：登录后才显示；管理入口按权限键决定。 */
export function userNav(me: Me | null): NavItem[] {
  if (me === null) return [];
  return sorted('user')
    .filter((page) => canSee(me, page))
    .map(toItem);
}
