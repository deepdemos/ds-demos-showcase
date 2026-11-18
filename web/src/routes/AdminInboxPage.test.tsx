import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router';
import { beforeEach, describe, expect, test, vi } from 'vitest';
import { getAdminInbox } from '@/api';
import { AdminInboxPage } from './AdminInboxPage';

const auth = vi.hoisted(() => ({
  me: null as null | { sub: string; username: string },
}));

vi.mock('@/features/auth/AuthContext', () => ({
  useAuth: () => ({ ...auth, isLoading: false, isError: false, setMe: vi.fn(), signOut: vi.fn() }),
}));

vi.mock('@/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/api')>();
  return { ...actual, getAdminInbox: vi.fn() };
});

const queueItem = (slug: string, title: string) => ({
  slug,
  title,
  description: null,
  authorName: '作者',
  moderationState: 'pending',
  submittedAt: '2026-11-18T00:00:00Z',
  previewPath: '/preview/' + slug + '/',
});

function renderPage() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={['/admin/inbox']}>
        <Routes>
          <Route path="/admin/inbox" element={<AdminInboxPage />} />
          <Route path="/login" element={<div>登录页</div>} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  auth.me = { sub: 'u1', username: 'alice' };
  vi.mocked(getAdminInbox).mockReset();
});

describe('AdminInboxPage', () => {
  test('三组待办、计数与条目都渲染', async () => {
    vi.mocked(getAdminInbox).mockResolvedValue({
      status: 200,
      data: {
        demoPendingCount: 2,
        approvedUnpublishedCount: 1,
        proposalPendingCount: 1,
        demoPending: [queueItem('demo-a', '待审作品')],
        approvedUnpublished: [queueItem('demo-b', '待发布作品')],
        proposals: [
          {
            id: 1,
            kind: 'new_task',
            title: '提案标题',
            source: 'user',
            createdAt: '2026-11-18T00:00:00Z',
          },
        ],
      },
    } as never);

    renderPage();

    expect(await screen.findByRole('heading', { name: '待审作品' })).toBeInTheDocument();
    expect(screen.getAllByText('已通过待发布').length).toBeGreaterThanOrEqual(1);
    expect(screen.getByText('提案标题')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: '待审作品' })).toHaveAttribute('href', '/demos/demo-a');
    expect(screen.getByRole('link', { name: '待发布作品' })).toHaveAttribute(
      'href',
      '/demos/demo-b',
    );
    await waitFor(() => expect(screen.getAllByText('2').length).toBeGreaterThanOrEqual(1));
  });

  test('403 显示没有审核权限', async () => {
    vi.mocked(getAdminInbox).mockResolvedValue({
      status: 403,
      data: { code: 'forbidden', message: '没有权限' },
    } as never);

    renderPage();

    expect(await screen.findByText('收件箱加载失败')).toBeInTheDocument();
    expect(screen.getByText('没有审核权限')).toBeInTheDocument();
  });

  test('未登录跳登录页', () => {
    auth.me = null;
    renderPage();
    expect(screen.getByText('登录页')).toBeInTheDocument();
    expect(getAdminInbox).not.toHaveBeenCalled();
  });
});
