import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router';
import { beforeEach, describe, expect, test, vi } from 'vitest';
import type { Me } from '@/api';
import { getUnreadNotificationCount } from '@/api';
import { AppShell } from './AppShell';

const auth = vi.hoisted(() => ({
  me: null as Me | null,
  isLoading: false,
  signOut: vi.fn(),
}));

vi.mock('@/features/auth/AuthContext', () => ({
  useAuth: () => ({
    ...auth,
    setMe: vi.fn(),
    isError: false,
  }),
}));

vi.mock('@/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/api')>();
  return { ...actual, getUnreadNotificationCount: vi.fn() };
});

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

function renderShell() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={['/']}>
        <Routes>
          <Route element={<AppShell />}>
            <Route path="/" element={<div>正文</div>} />
          </Route>
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  auth.me = me();
  auth.isLoading = false;
  vi.mocked(getUnreadNotificationCount).mockReset();
  vi.mocked(getUnreadNotificationCount).mockResolvedValue({
    status: 200,
    data: { count: 3 },
  } as never);
});

describe('AppShell 导航', () => {
  test('一级入口是作品库 / 排行榜 / 投稿', () => {
    renderShell();
    const nav = screen.getByRole('navigation', { name: '主导航' });
    expect(within(nav).getByRole('link', { name: '作品库' })).toHaveAttribute('href', '/demos');
    expect(within(nav).getByRole('link', { name: '探索' })).toHaveAttribute('href', '/explore');
    expect(within(nav).getByRole('link', { name: '排行榜' })).toHaveAttribute('href', '/rankings');
    expect(within(nav).getByRole('link', { name: '投稿' })).toHaveAttribute('href', '/submit');
  });

  test('登录后用用户菜单放个人入口与通知角标', async () => {
    renderShell();
    fireEvent.click(screen.getByRole('button', { name: 'Alice' }));
    await screen.findByRole('menuitem', { name: /通知/ });
    await waitFor(() =>
      expect(screen.getByRole('menuitem', { name: /通知/ })).toHaveTextContent('3'),
    );
    expect(screen.getByRole('menuitem', { name: '我的作品' })).toHaveAttribute('href', '/me/demos');
    expect(screen.getByRole('menuitem', { name: '登出' })).toBeInTheDocument();
  });

  test('管理员在用户菜单里看到管理入口', async () => {
    auth.me = me(['demo.review']);
    renderShell();
    fireEvent.click(screen.getByRole('button', { name: 'Alice' }));
    expect(await screen.findByRole('menuitem', { name: '管理' })).toHaveAttribute(
      'href',
      '/admin/inbox',
    );
  });

  test('访客看到登录按钮，没有用户菜单', () => {
    auth.me = null;
    renderShell();
    expect(screen.getByRole('button', { name: '登录' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Alice' })).toBeNull();
  });

  test('移动端菜单包含一级入口与登录', () => {
    auth.me = null;
    renderShell();
    fireEvent.click(screen.getByRole('button', { name: '菜单' }));
    const nav = screen.getByRole('navigation', { name: '主导航（移动端）' });
    expect(within(nav).getByRole('link', { name: '作品库' })).toBeInTheDocument();
    expect(screen.getAllByRole('button', { name: '登录' }).length).toBeGreaterThan(0);
  });
});
