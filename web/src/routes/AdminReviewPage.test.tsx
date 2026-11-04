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
// 页面通过 PublishCard 间接用到 publishDemo；这条 mock 只在渲染「待发布」区块时被碰，
// 但这个用例的通过动作会让它出现，所以必须给个桩，否则 import 到 undefined 会炸。
const publishDemo = vi.hoisted(() => vi.fn());

vi.mock('@/api', () => ({ listDemoReviewQueue, reviewDemo, publishDemo }));

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

  test('401 展示「登录已过期」并给出登录去处', async () => {
    auth.me = { sub: 'x', username: 'u' };
    // 会话过期时 /me 的缓存还没失效，所以页面会进到这里而不是跳登录。
    // 以前只判 403，401 会渲染成一片空白——既没有错误态也没有队列。
    listDemoReviewQueue.mockResolvedValue({ status: 401, data: { message: '未登录' } });
    renderPage();

    expect(await screen.findByText('登录已过期')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: '去登录' })).toHaveAttribute(
      'href',
      '/login?return_to=/admin/reviews',
    );
    // 401 不能和 403 混：说「没有审核权限」会让人去申请权限，而实际上只是要重新登录。
    expect(screen.queryByText('没有审核权限')).not.toBeInTheDocument();
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

  test('非 2xx 也要报错——orval 非 2xx 不 throw', async () => {
    auth.me = { sub: 'm', username: 'mod' };
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
    // 409 = 这条作品刚被别人审过。orval 会照常 resolve，所以页面必须自己看 status。
    reviewDemo.mockResolvedValue({ status: 409, data: { message: '作品不处于待审状态' } });

    renderPage();
    fireEvent.click(await screen.findByRole('button', { name: '通过' }));

    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent('作品不处于待审状态');
    });
  });
});
