import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router';
import { beforeEach, describe, expect, test, vi } from 'vitest';
import { AdminReviewPage } from './AdminReviewPage';

const auth = vi.hoisted(() => ({ me: null as null | { sub: string; username: string } }));

vi.mock('@/features/auth/AuthContext', () => ({
  useAuth: () => ({ ...auth, isLoading: false, isError: false, setMe: vi.fn(), signOut: vi.fn() }),
}));

const listDemoReviewQueue = vi.hoisted(() => vi.fn());
const reviewDemo = vi.hoisted(() => vi.fn());

vi.mock('@/api', () => ({ listDemoReviewQueue, reviewDemo }));

function renderPage() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={['/admin/reviews']}>
        <Routes>
          <Route path="/admin/reviews" element={<AdminReviewPage />} />
          <Route path="/login" element={<div>登录页</div>} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  listDemoReviewQueue.mockReset();
  reviewDemo.mockReset();
});

describe('AdminReviewPage', () => {
  test('未登录跳登录页', () => {
    auth.me = null;
    renderPage();
    expect(screen.getByText('登录页')).toBeInTheDocument();
    expect(listDemoReviewQueue).not.toHaveBeenCalled();
  });

  test('403 展示「没有审核权限」，不展示空态', async () => {
    auth.me = { sub: 'x', username: 'u' };
    listDemoReviewQueue.mockResolvedValue({ status: 403, data: { message: '没有权限' } });
    renderPage();

    expect(await screen.findByText('没有审核权限')).toBeInTheDocument();
    // 关键：403 不能渲染成「没有待审作品」——那会让审核员以为队列是空的。
    expect(screen.queryByText('没有待审作品')).not.toBeInTheDocument();
  });

  test('队列为空时给出空态', async () => {
    auth.me = { sub: 'x', username: 'u' };
    listDemoReviewQueue.mockResolvedValue({ status: 200, data: { items: [] } });
    renderPage();
    expect(await screen.findByText('没有待审作品')).toBeInTheDocument();
  });

  test('点通过与驳回都会带上对应 decision', async () => {
    auth.me = { sub: 'x', username: 'u' };
    listDemoReviewQueue.mockResolvedValue({
      status: 200,
      data: {
        items: [
          {
            slug: 'demo-abc',
            title: '一个作品',
            description: null,
            authorName: 'someone',
            moderationState: 'pending',
            submittedAt: '2026-11-01T00:00:00Z',
            previewPath: '/preview/demo-abc/v1/',
          },
        ],
      },
    });
    reviewDemo.mockResolvedValue({ status: 200, data: {} });
    renderPage();

    expect(await screen.findByText('一个作品')).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: '通过' }));
    // mutation 是异步的，点完立刻断言会读到「还没发出去」。
    await waitFor(() =>
      expect(reviewDemo).toHaveBeenCalledWith('demo-abc', {
        decision: 'approve',
        reason: undefined,
      }),
    );

    fireEvent.click(screen.getByRole('button', { name: '驳回' }));
    await waitFor(() =>
      expect(reviewDemo).toHaveBeenCalledWith('demo-abc', {
        decision: 'reject',
        reason: undefined,
      }),
    );
  });

  test('预览链接指向版本化路径且开新标签页', async () => {
    auth.me = { sub: 'x', username: 'u' };
    listDemoReviewQueue.mockResolvedValue({
      status: 200,
      data: {
        items: [
          {
            slug: 'demo-abc',
            title: '一个作品',
            description: null,
            authorName: null,
            moderationState: 'pending',
            submittedAt: '2026-11-01T00:00:00Z',
            previewPath: '/preview/demo-abc/v1/',
          },
        ],
      },
    });
    renderPage();

    const link = await screen.findByRole('link', { name: '预览' });
    expect(link).toHaveAttribute('href', '/preview/demo-abc/v1/');
    expect(link).toHaveAttribute('target', '_blank');
  });
});
