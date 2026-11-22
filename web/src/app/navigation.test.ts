import { describe, expect, test } from 'vitest';
import type { Me } from '@/api';
import {
  adminNav,
  hasAdminAccess,
  hasAnyPermission,
  meNav,
  primaryNav,
  userNav,
} from './navigation';

function me(permissions: string[] = [], role = 'user'): Me {
  return {
    sub: 'u1',
    username: 'alice',
    displayName: 'Alice',
    email: 'a@example.com',
    role,
    status: 'active',
    level: 0,
    createdAt: '2026-01-01T00:00:00+08:00',
    permissions,
  } as Me;
}

describe('导航派生', () => {
  test('一级入口按 order 排列', () => {
    expect(primaryNav(null).map((item) => item.label)).toEqual([
      '作品库',
      '探索',
      '排行榜',
      '投稿',
    ]);
    expect(primaryNav(null).map((item) => item.to)).toEqual([
      '/demos',
      '/explore',
      '/rankings',
      '/submit',
    ]);
  });

  test('访客没有个人菜单', () => {
    expect(userNav(null)).toEqual([]);
    expect(meNav(null)).toEqual([]);
    expect(adminNav(null)).toEqual([]);
  });

  test('个人区二级导航使用 /me 地址，不混入管理入口', () => {
    const items = meNav(me(['demo.review']));
    expect(items.map((item) => item.label)).toEqual(['我的作品', '收藏夹', '浏览历史', '通知']);
    expect(items.map((item) => item.to)).toEqual([
      '/me/demos',
      '/me/collections',
      '/me/history',
      '/me/notifications',
    ]);
  });

  test('登录用户没有管理权限时，用户菜单与个人导航一致', () => {
    expect(userNav(me()).map((item) => item.to)).toEqual([
      '/me/demos',
      '/me/collections',
      '/me/history',
      '/me/notifications',
    ]);
  });

  test('有任一管理权限时出现管理入口，落到收件箱', () => {
    expect(hasAdminAccess(me(['demo.review']))).toBe(true);
    expect(hasAdminAccess(me())).toBe(false);
    const admin = userNav(me(['demo.review'])).find((item) => item.id === 'admin');
    expect(admin).toMatchObject({ label: '管理', to: '/admin/inbox' });
  });

  test('角色是 admin / moderator 时也视为管理区可达', () => {
    expect(hasAdminAccess(me([], 'admin'))).toBe(true);
    expect(hasAdminAccess(me([], 'moderator'))).toBe(true);
  });

  test('管理导航按权限过滤，并带上分组', () => {
    const review = adminNav(me(['demo.review']));
    expect(review.map((item) => item.label)).toEqual(['收件箱', '作品审核']);
    expect(review.every((item) => item.group === '待办')).toBe(true);
    expect(review.map((item) => item.to)).toEqual(['/admin/inbox', '/admin/reviews']);

    const models = adminNav(me(['model.import']));
    expect(models.map((item) => item.label)).toEqual(['模型导入']);
    expect(models[0]?.group).toBe('内容');
  });

  test('admin 角色看到全部管理入口', () => {
    expect(adminNav(me([], 'admin')).map((item) => item.label)).toEqual([
      '收件箱',
      '作品审核',
      '申诉',
      '模型导入',
    ]);
  });

  test('权限键是精确匹配', () => {
    expect(hasAnyPermission(me(['demo.review']), ['demo.publish'])).toBe(false);
    expect(hasAnyPermission(me(['demo.publish']), ['demo.publish', 'demo.review'])).toBe(true);
  });
});
