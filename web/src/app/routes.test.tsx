import { describe, expect, test } from 'vitest';
import { childPath, pages } from './routes';
import { routeElements } from './route-elements';

const WIDTHS = ['auth', 'form', 'narrow', 'default', 'wide'];
const GROUPS = ['public', 'me', 'admin', 'auth', 'system'];

describe('路由清单', () => {
  test('id 与 path 都唯一', () => {
    expect(new Set(pages.map((page) => page.id)).size).toBe(pages.length);
    expect(new Set(pages.map((page) => page.path)).size).toBe(pages.length);
  });

  test('每条路由都有页面元素，且元素表没有多余项', () => {
    const ids = pages.map((page) => page.id).sort();
    expect(Object.keys(routeElements).sort()).toEqual(ids);
    for (const page of pages) {
      expect(routeElements[page.id], page.id).toBeTruthy();
    }
  });

  test('路径格式正确，404 只有一条', () => {
    for (const page of pages) {
      expect(page.path === '*' || page.path.startsWith('/'), page.id).toBe(true);
      expect(page.title.length, page.id).toBeGreaterThan(0);
      expect(GROUPS).toContain(page.group);
      expect(WIDTHS).toContain(page.width);
      expect(['ready', 'fixture']).toContain(page.availability);
    }
    expect(pages.filter((page) => page.path === '*')).toHaveLength(1);
    expect(pages.find((page) => page.path === '*')?.group).toBe('system');
  });

  test('childPath 把绝对路径转成嵌套路由的相对路径', () => {
    expect(childPath('/')).toBe('');
    expect(childPath('/demos')).toBe('demos');
    expect(childPath('/admin/models')).toBe('admin/models');
  });

  test('受保护页面不是 public', () => {
    const protectedIds = ['submit', 'my-demos', 'history', 'collections', 'notifications'];
    for (const id of protectedIds) {
      const page = pages.find((entry) => entry.id === id);
      expect(page, id).toBeTruthy();
      expect(page?.access.kind, id).not.toBe('public');
    }
  });
});
