import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router';
import { beforeEach, describe, expect, test, vi } from 'vitest';
import { getUnreadNotificationCount } from '@/api';
import { AppLayout } from './AppLayout';

const auth = vi.hoisted(() => ({
  me: null as null | { sub: string; username: string; role: string },
}));

vi.mock('@/features/auth/AuthContext', () => ({
  useAuth: () => ({ ...auth, isLoading: false, isError: false, setMe: vi.fn(), signOut: vi.fn() }),
}));

vi.mock('@/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/api')>();
  return { ...actual, getUnreadNotificationCount: vi.fn() };
});

function renderLayout() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={['/']}>
        <Routes>
          <Route element={<AppLayout />}>
            <Route path="/" element={<div>首页</div>} />
          </Route>
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  auth.me = { sub: 'u1', username: 'alice', role: 'user' };
  vi.mocked(getUnreadNotificationCount).mockReset();
});

describe('AppLayout 通知角标', () => {
  test('有未读时显示数字角标', async () => {
    vi.mocked(getUnreadNotificationCount).mockResolvedValue({
      status: 200,
      data: { count: 3 },
    } as never);

    renderLayout();

    expect(await screen.findByLabelText('未读通知 3')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: /通知/ })).toHaveAttribute('href', '/notifications');
  });

  test('未读为 0 时不显示角标', async () => {
    vi.mocked(getUnreadNotificationCount).mockResolvedValue({
      status: 200,
      data: { count: 0 },
    } as never);

    renderLayout();

    await screen.findByText('首页');
    expect(screen.queryByLabelText(/未读通知/)).toBeNull();
  });
});
