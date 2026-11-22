import { describe, expect, test } from 'vitest';
import type { Me } from '@/api';
import { hasAdminAccess, hasAnyPermission, primaryNav, userNav } from './navigation';

function me(permissions: string[] = []): Me {
  return {
    sub: 'u1',
    username: 'alice',
    displayName: 'Alice',
    email: 'a@example.com',
    role: 'user',
    status: 'active',
    level: 0,
    createdAt: '2026-01-01T00:00:00+08:00',
    permissions,
  } as Me;
}

describe('导航派生', () => {
  test('一级入口按 order 排列', () => {
    expect(primaryNav(null).map((item) => item.label)).toEqual(['作品库', '排行榜', '投稿']);
    expect(primaryNav(null).map((item) => item.to)).toEqual(['/demos', '/rankings', '/submit']);
  });

  test('访客没有个人菜单', () => {
    expect(userNav(null)).toEqual([]);
  });

  test('登录用户有个人入口，但没有管理入口', () => {
    const labels = userNav(me()).map((item) => item.label);
    expect(labels).toEqual(['我的作品', '收藏夹', '浏览历史', '通知']);
    expect(labels).not.toContain('管理');
  });

  test('有任一管理权限时出现管理入口', () => {
    expect(hasAdminAccess(me(['demo.review']))).toBe(true);
    expect(hasAdminAccess(me())).toBe(false);
    expect(userNav(me(['demo.review'])).map((item) => item.label)).toContain('管理');
  });

  test('权限键是精确匹配', () => {
    expect(hasAnyPermission(me(['demo.review']), ['demo.publish'])).toBe(false);
    expect(hasAnyPermission(me(['demo.publish']), ['demo.publish', 'demo.review'])).toBe(true);
  });
});
