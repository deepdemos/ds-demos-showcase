import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, waitFor } from '@testing-library/react';
import { createMemoryRouter, RouterProvider } from 'react-router';
import { beforeEach, describe, expect, test, vi } from 'vitest';
import type { Me } from '@/api';
import { routes } from './router';

const auth = vi.hoisted(() => ({
  me: null as Me | null,
  isLoading: false,
}));

vi.mock('@/features/auth/AuthContext', () => ({
  useAuth: () => ({
    me: auth.me,
    isLoading: auth.isLoading,
    isError: false,
    setMe: vi.fn(),
    signOut: vi.fn(),
  }),
}));

function renderAt(path: string) {
  const router = createMemoryRouter(routes, { initialEntries: [path] });
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  render(
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
  return router;
}

beforeEach(() => {
  auth.me = null;
  auth.isLoading = false;
  localStorage.clear();
});

describe('真实路由树', () => {
  test('访客能打开本机浏览历史（HIST-001）', async () => {
    renderAt('/me/history');
    expect(await screen.findByRole('heading', { level: 1, name: '浏览历史' })).toBeInTheDocument();
    expect(screen.getByText(/本机记录只保存在这个浏览器/)).toBeInTheDocument();
  });

  test('访客访问我的作品会回登录并保留 return_to', async () => {
    const router = renderAt('/me/demos');
    await waitFor(() => expect(router.state.location.pathname).toBe('/login'));
    expect(router.state.location.search).toContain('return_to=%2Fme%2Fdemos');
    expect(await screen.findByRole('heading', { name: '登录' })).toBeInTheDocument();
  });

  test('旧地址重定向保留 query 与 hash', async () => {
    const router = renderAt('/my/demos?state=deleted#top');
    await waitFor(() => expect(router.state.location.pathname).toBe('/login'));
    expect(router.state.location.search).toContain(
      'return_to=%2Fme%2Fdemos%3Fstate%3Ddeleted%23top',
    );
  });

  test('未匹配路径显示 404', async () => {
    renderAt('/no-such-page');
    expect(await screen.findByRole('heading', { name: '404' })).toBeInTheDocument();
  });
});
