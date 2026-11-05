import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { expect, test, vi } from 'vitest';
import { archiveDemo } from '@/api';
import type { DemoDetail } from '@/api';
import { LifecycleBar } from './DemoDetailPage';

vi.mock('@/api', () => ({
  archiveDemo: vi.fn(),
  unarchiveDemo: vi.fn(),
  deleteDemo: vi.fn(),
  restoreDemo: vi.fn(),
}));

function demoWith(actions: Partial<DemoDetail['lifecycleActions']>): DemoDetail {
  return {
    slug: 'demo-x',
    title: '标题',
    coverUrl: null,
    createdAt: '2026-11-01T00:00:00Z',
    lifecycleState: 'published',
    description: '简介',
    updatedAt: '2026-11-01T00:00:00Z',
    demoType: 'web',
    downloadable: true,
    canEdit: true,
    lifecycleActions: {
      archive: false,
      unarchive: false,
      delete: false,
      restore: false,
      ...actions,
    },
  } as DemoDetail;
}

function renderBar(demo: DemoDetail) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter>
        <LifecycleBar slug={demo.slug} demo={demo} />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

/**
 * 四个按钮一个都不该出现时，整块不该占位。
 *
 * 访客、被平台移除的作品、已擦除的作品都会落到这个分支——多渲染一个空段落，
 * 页面上就会多出一条莫名的间距。
 */
test('没有任何可用动作时整块不渲染', () => {
  const { container } = renderBar(demoWith({}));
  expect(container.querySelector('section[aria-label="作品管理"]')).toBeNull();
});

test('已发布且有权限：出下架与删除，不出重新上架', () => {
  renderBar(demoWith({ archive: true, delete: true }));
  expect(screen.getByRole('button', { name: '下架作品' })).toBeTruthy();
  expect(screen.getByRole('button', { name: '删除作品' })).toBeTruthy();
  expect(screen.queryByRole('button', { name: '重新上架' })).toBeNull();
});

/**
 * 删除是两步：点「删除作品」只展开确认，不直接删。
 *
 * 这一条钉的是「误点一次不会毁掉作品」——删除对外立刻不可见，
 * 恢复还要受窗口限制，不该是一个单击的结果。
 */
test('删除要点两次，第一次只展开确认', () => {
  renderBar(demoWith({ delete: true }));
  fireEvent.click(screen.getByRole('button', { name: '删除作品' }));
  expect(screen.getByRole('button', { name: '确认删除' })).toBeTruthy();
  expect(screen.getByRole('button', { name: '取消' })).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: '取消' }));
  expect(screen.queryByRole('button', { name: '确认删除' })).toBeNull();
});

/**
 * 后端拒绝时把后端原话显示出来。
 *
 * orval 对非 2xx 也 resolve，如果这里不看 `status`，409 会被当成成功——
 * 按钮点完什么也没变，还没人知道为什么。
 */
test('下架被拒时显示后端给的原文', async () => {
  vi.mocked(archiveDemo).mockResolvedValueOnce({
    status: 409,
    data: { code: 'conflict', message: '只有已发布的作品能下架，当前是 archived' },
  } as never);

  renderBar(demoWith({ archive: true }));
  fireEvent.click(screen.getByRole('button', { name: '下架作品' }));

  await waitFor(() => {
    expect(screen.getByRole('alert').textContent).toContain('只有已发布的作品能下架');
  });
});
