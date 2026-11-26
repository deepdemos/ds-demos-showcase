import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router';
import { beforeEach, describe, expect, test, vi } from 'vitest';
import {
  listNotifications,
  markAllNotificationsRead,
  markNotificationRead,
  type Notification,
} from '@/api';
import { NotificationsPage } from './NotificationsPage';

const auth = vi.hoisted(() => ({
  me: null as null | { sub: string; username: string },
}));

vi.mock('@/features/auth/AuthContext', () => ({
  useAuth: () => ({ ...auth, isLoading: false, isError: false, setMe: vi.fn(), signOut: vi.fn() }),
}));

vi.mock('@/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/api')>();
  return {
    ...actual,
    listNotifications: vi.fn(),
    markNotificationRead: vi.fn(),
    markAllNotificationsRead: vi.fn(),
  };
});

const notification = (over: Partial<Notification> = {}): Notification => ({
  id: 1,
  kind: 'review.approved',
  objectType: 'demo',
  objectKey: 'demo-a',
  objectTitle: '一号作品',
  demoSlug: 'demo-a',
  demoTitle: '一号作品',
  payload: {},
  readAt: null,
  createdAt: '2026-11-18T00:00:00Z',
  accessible: true,
  ...over,
});

function listPayload(
  items: Notification[],
  unreadCount = items.filter((i) => i.readAt === null).length,
) {
  return {
    status: 200,
    data: {
      items,
      total: items.length,
      unreadCount,
      page: 1,
      pageSize: 20,
    },
  };
}

function renderPage() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={['/notifications']}>
        <Routes>
          <Route path="/notifications" element={<NotificationsPage />} />
          <Route path="/login" element={<div>登录页</div>} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  auth.me = { sub: 'u1', username: 'alice' };
  vi.mocked(listNotifications).mockReset();
  vi.mocked(markNotificationRead).mockReset();
  vi.mocked(markAllNotificationsRead).mockReset();
});

describe('NotificationsPage', () => {
  test('按 kind 组装文案，墓碑作品不给跳转', async () => {
    vi.mocked(listNotifications).mockResolvedValue(
      listPayload([
        notification({ id: 1, kind: 'review.approved' }),
        notification({
          id: 2,
          kind: 'review.rejected',
          objectKey: 'demo-b',
          objectTitle: '二号作品',
          demoSlug: 'demo-b',
          demoTitle: '二号作品',
          payload: { reason: '封面不合格' },
          readAt: '2026-11-18T01:00:00Z',
          accessible: false,
        }),
        notification({
          id: 3,
          kind: 'rating.received',
          objectKey: 'demo-c',
          objectTitle: '三号作品',
          demoSlug: 'demo-c',
          demoTitle: '三号作品',
          payload: { score: 4 },
          accessible: false,
        }),
      ]) as never,
    );

    renderPage();

    expect(await screen.findByText(/《一号作品》已通过审核/)).toBeInTheDocument();
    expect(screen.getByText(/《二号作品》被驳回/)).toBeInTheDocument();
    expect(screen.getByText('理由：封面不合格')).toBeInTheDocument();
    expect(screen.getByText(/有人给《三号作品》打了 4 分/)).toBeInTheDocument();
    expect(screen.getAllByText('该作品已不可访问')).toHaveLength(2);
    expect(screen.getAllByRole('link', { name: '查看作品' })).toHaveLength(1);
  });

  test('非作品对象按对象类型渲染：提案不给作品链接', async () => {
    // 旧字段（demoSlug / demoTitle）对非作品对象是空串——渲染不能因此塌掉。
    vi.mocked(listNotifications).mockResolvedValue(
      listPayload([
        notification({
          id: 1,
          kind: 'model.proposal.rejected',
          objectType: 'proposal',
          objectKey: '42',
          objectTitle: '示例模型',
          demoSlug: '',
          demoTitle: '',
          payload: { reason: '和已有条目重复' },
        }),
        notification({
          id: 2,
          kind: 'model.proposal.approved',
          objectType: 'model',
          objectKey: 'deepseek-chat',
          objectTitle: 'DeepSeek Chat',
          demoSlug: '',
          demoTitle: '',
          readAt: '2026-11-18T02:00:00Z',
        }),
      ]) as never,
    );

    renderPage();

    expect(await screen.findByText(/《示例模型》的收录提案被驳回/)).toBeInTheDocument();
    expect(screen.getByText('理由：和已有条目重复')).toBeInTheDocument();
    expect(screen.getByText(/《DeepSeek Chat》的收录提案已通过/)).toBeInTheDocument();
    // 提案没有作者可去的页面（#595 定入口），模型有模型页；两种都不该冒出一条作品链接。
    expect(screen.queryByRole('link', { name: '查看作品' })).not.toBeInTheDocument();
    const modelLink = screen.getByRole('link', { name: '查看模型' });
    expect(modelLink).toHaveAttribute('href', '/models/deepseek-chat');
    expect(screen.getAllByRole('link')).toHaveLength(1);
  });

  test('标为已读调用单项接口', async () => {
    vi.mocked(listNotifications).mockResolvedValue(listPayload([notification()]) as never);
    vi.mocked(markNotificationRead).mockResolvedValue({ status: 204, data: undefined } as never);

    renderPage();
    fireEvent.click(await screen.findByRole('button', { name: /标为已读/ }));

    await waitFor(() => expect(markNotificationRead).toHaveBeenCalledWith(1));
  });

  test('全部已读调用接口；没有未读时按钮禁用', async () => {
    vi.mocked(listNotifications).mockResolvedValue(listPayload([notification()]) as never);
    vi.mocked(markAllNotificationsRead).mockResolvedValue({
      status: 204,
      data: undefined,
    } as never);

    renderPage();
    const all = await screen.findByRole('button', { name: '全部已读' });
    // 列表没回来之前按钮是 disabled，先等它可用再点。
    await waitFor(() => expect(all).toBeEnabled());
    fireEvent.click(all);
    await waitFor(() => expect(markAllNotificationsRead).toHaveBeenCalledTimes(1));
  });

  test('未登录跳登录页', () => {
    auth.me = null;
    renderPage();
    expect(screen.getByText('登录页')).toBeInTheDocument();
    expect(listNotifications).not.toHaveBeenCalled();
  });
});
