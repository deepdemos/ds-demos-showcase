import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { describe, expect, test } from 'vitest';
import { pageForPath } from '@/app/RouteTitle';
import { fixtureElements, fixturePages } from './fixtures';

describe('DEV fixtures', () => {
  test('11 条页面都有唯一路径与元素', () => {
    expect(fixturePages).toHaveLength(11);
    expect(new Set(fixturePages.map((page) => page.id)).size).toBe(11);
    expect(new Set(fixturePages.map((page) => page.path)).size).toBe(11);
    for (const page of fixturePages) {
      expect(page.availability).toBe('fixture');
      expect(fixtureElements[page.id]).toBeTruthy();
    }
  });

  test('按区分布：公开 2、个人 2、管理 7', () => {
    expect(fixturePages.filter((page) => page.group === 'public')).toHaveLength(2);
    expect(fixturePages.filter((page) => page.group === 'me')).toHaveLength(2);
    expect(fixturePages.filter((page) => page.group === 'admin')).toHaveLength(7);
  });

  test('DEV 下标题能匹配 fixture 路径', () => {
    expect(pageForPath('/benchmarks').title).toBe('基准');
    expect(pageForPath('/benchmarks/mmlu').title).toBe('基准详情');
    expect(pageForPath('/admin/audit/42').title).toBe('审计事件');
  });

  test('无参数页面都能渲染到 Page 标题', () => {
    const dynamic = new Set(['fixture-benchmark-detail', 'fixture-admin-audit-detail']);
    for (const [id, element] of Object.entries(fixtureElements)) {
      if (dynamic.has(id)) continue;
      const view = render(<MemoryRouter>{element}</MemoryRouter>);
      expect(screen.getByRole('heading', { level: 1 })).toBeInTheDocument();
      view.unmount();
    }
  });
});
