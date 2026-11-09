import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router';
import { beforeEach, expect, test, vi } from 'vitest';

import { listMyDemos } from '@/api';
import { MyDemosPage, rowState } from './MyDemosPage';
import type { MyDemoSummary } from '@/api';

const auth = vi.hoisted(() => ({
  me: { id: 1, username: 'author', displayName: '作者', role: 'user' } as {
    id: number;
    username: string;
    displayName: string;
    role: string;
  } | null,
  isLoading: false,
}));

vi.mock('@/features/auth/AuthContext', () => ({
  useAuth: () => ({ me: auth.me, isLoading: auth.isLoading }),
}));

vi.mock('@/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/api')>();
  return { ...actual, listMyDemos: vi.fn() };
});

function demo(over: Partial<MyDemoSummary> = {}): MyDemoSummary {
  return {
    slug: 'demo-a',
    title: '一件作品',
    coverUrl: null,
    createdAt: '2026-11-01T10:00:00Z',
    updatedAt: '2026-11-02T10:00:00Z',
    lifecycleState: 'published',
    moderationState: 'approved',
    deletedAt: null,
    purgeAfter: null,
    ...over,
  } as MyDemoSummary;
}

function renderPage(entry = '/my/demos') {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[entry]}>
        <Routes>
          <Route path="/my/demos" element={<MyDemosPage />} />
          <Route path="/login" element={<p>登录页</p>} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  auth.me = { id: 1, username: 'author', displayName: '作者', role: 'user' };
  auth.isLoading = false;
  vi.mocked(listMyDemos).mockResolvedValue({
    status: 200,
    data: { items: [demo()], page: 1, pageSize: 20, total: 1 },
  } as never);
});

test('已上线的作品链到读者看的页面', async () => {
  renderPage();
  const link = await screen.findByRole('link', { name: '一件作品' });
  expect(link).toHaveAttribute('href', '/demos/demo-a');
  // 收窄到列表里找：筛选下拉的选项也叫「已上线」。
  expect(within(screen.getByRole('list')).getByText('已上线')).toBeInTheDocument();
});

// 草稿没有对外版本，详情接口对它 404（作者也一样）。给错链接的症状是「刚投完稿点进去 404」。
test('草稿链到预览，而不是详情', async () => {
  vi.mocked(listMyDemos).mockResolvedValue({
    status: 200,
    data: {
      items: [demo({ lifecycleState: 'draft', moderationState: 'pending' })],
      page: 1,
      pageSize: 20,
      total: 1,
    },
  } as never);

  renderPage();
  const link = await screen.findByRole('link', { name: '一件作品' });
  expect(link).toHaveAttribute('href', '/preview/demo-a/');
  expect(within(screen.getByRole('list')).getByText('待审核')).toBeInTheDocument();
});

// 软删不改 lifecycle_state（只写 deleted_at），先判生命周期会得出「已上线」这种误导结论。
test('软删优先于生命周期：显示已删除、没有链接、带清空时间', async () => {
  vi.mocked(listMyDemos).mockResolvedValue({
    status: 200,
    data: {
      items: [
        demo({
          lifecycleState: 'published',
          deletedAt: '2026-11-05T10:00:00Z',
          purgeAfter: '2026-12-05T10:00:00Z',
        }),
      ],
      page: 1,
      pageSize: 20,
      total: 1,
    },
  } as never);

  renderPage();
  await screen.findByRole('list');
  expect(within(screen.getByRole('list')).getByText('已删除')).toBeInTheDocument();
  expect(screen.queryByRole('link', { name: '一件作品' })).toBeNull();
  expect(screen.getByText(/2026\/12\/5 清空/)).toBeInTheDocument();
});

test('已上线但新版本待审时，说明线上仍是上一版', async () => {
  vi.mocked(listMyDemos).mockResolvedValue({
    status: 200,
    data: {
      items: [demo({ moderationState: 'pending' })],
      page: 1,
      pageSize: 20,
      total: 1,
    },
  } as never);

  renderPage();
  expect(await screen.findByText('已上线 · 新版本待审')).toBeInTheDocument();
  expect(screen.getByText(/线上仍是上一版/)).toBeInTheDocument();
});

test('没有作品时给投稿入口', async () => {
  vi.mocked(listMyDemos).mockResolvedValue({
    status: 200,
    data: { items: [], page: 1, pageSize: 20, total: 0 },
  } as never);

  renderPage();
  expect(await screen.findByText('还没有投稿过作品')).toBeInTheDocument();
  expect(screen.getByRole('link', { name: '去投稿' })).toHaveAttribute('href', '/submit');
});

test('未登录时跳登录页，且不发请求', async () => {
  auth.me = null;
  renderPage();
  expect(await screen.findByText('登录页')).toBeInTheDocument();
  expect(vi.mocked(listMyDemos)).not.toHaveBeenCalled();
});

test('读失败时显示错误与重试', async () => {
  vi.mocked(listMyDemos).mockResolvedValue({ status: 503, data: {} } as never);
  renderPage();
  expect(await screen.findByRole('alert')).toHaveTextContent('我的作品加载失败（HTTP 503）');
  expect(screen.getByRole('button', { name: '重试' })).toBeInTheDocument();
});

test('rowState 把三种 draft 形态区分开', () => {
  const draft = (m: MyDemoSummary['moderationState']) =>
    rowState(demo({ lifecycleState: 'draft', moderationState: m }));
  expect(draft('pending').label).toBe('待审核');
  expect(draft('rejected').label).toBe('被驳回');
  expect(draft('approved').label).toBe('已通过 · 待发布');
  expect(draft('pending').href).toBe('/preview/demo-a/');
});

// 筛选项是三个正交维度（生命周期 / 审核态 / 是否软删）的组合，组合留在前端：
// 接口不替界面决定「状态桶」，否则每加一个界面口径就要改一次契约。
test('选状态筛选时把对应的三个维度带上，并回到第 1 页', async () => {
  renderPage('/my/demos?page=3');
  await screen.findByRole('list');

  fireEvent.change(screen.getByLabelText('状态'), { target: { value: 'reviewing' } });

  await waitFor(() =>
    expect(vi.mocked(listMyDemos)).toHaveBeenLastCalledWith(
      expect.objectContaining({ page: 1, lifecycle: 'published', moderation: 'pending' }),
    ),
  );
});

test('「已删除」是独立一维，不带生命周期', async () => {
  renderPage();
  await screen.findByRole('list');

  fireEvent.change(screen.getByLabelText('状态'), { target: { value: 'deleted' } });

  await waitFor(() => {
    const call = vi.mocked(listMyDemos).mock.calls.at(-1)?.[0];
    expect(call).toEqual(expect.objectContaining({ deleted: true }));
    expect(call).not.toHaveProperty('lifecycle');
  });
});
