import { describe, expect, test } from 'vitest';
import { pageForPath } from './RouteTitle';

describe('pageForPath', () => {
  test('静态路径与动态路径都能匹配', () => {
    expect(pageForPath('/').title).toBe('首页');
    expect(pageForPath('/demos').title).toBe('作品库');
    expect(pageForPath('/demos/blackhole-merge').title).toBe('作品详情');
    expect(pageForPath('/demos/blackhole-merge/edit').title).toBe('编辑作品');
    expect(pageForPath('/me/collections/3').title).toBe('收藏夹详情');
    expect(pageForPath('/me/demos').title).toBe('我的作品');
    expect(pageForPath('/admin/inbox').title).toBe('收件箱');
  });

  test('未知路径回 404', () => {
    expect(pageForPath('/nope').title).toBe('页面不存在');
  });
});
