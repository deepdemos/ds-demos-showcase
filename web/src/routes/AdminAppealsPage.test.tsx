import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router';
import { beforeEach, describe, expect, test, vi } from 'vitest';
import { listAdminAppeals, reviewAdminAppeal } from '@/api';
import { AdminAppealsPage } from './AdminAppealsPage';

const auth = vi.hoisted(() => ({
  me: null as null | { sub: string; username: string },
}));

vi.mock('@/features/auth/AuthContext', () => ({
  useAuth: () => ({ ...auth, isLoading: false, isError: false, setMe: vi.fn(), signOut: vi.fn() }),
}));

vi.mock('@/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/api')>();
  return { ...actual, listAdminAppeals: vi.fn(), reviewAdminAppeal: vi.fn() };
});

const appeal = (over: Record<string, unknown> = {}) => ({
  id: 1,
  demoSlug: 'demo-a',
  demoTitle: '被移除作品',
  reason: '误判，请复核',
  status: 'pending',
  authorName: '作者',
  createdAt: '2026-11-18T00:00:00Z',
  reviewedAt: null,
  decisionReason: null,
  ...over,
});

function renderPage() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={['/admin/appeals']}>
        <Routes>
          <Route path="/admin/appeals" element={<AdminAppealsPage />} />
          <Route path="/login" element={<div>登录页</div>} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  auth.me = { sub: 'u1', username: 'alice' };
  vi.mocked(listAdminAppeals).mockReset();
  vi.mocked(reviewAdminAppeal).mockReset();
});

describe('AdminAppealsPage', () => {
  test('通过时带裁决说明调接口', async () => {
    vi.mocked(listAdminAppeals).mockResolvedValue({
      status: 200,
      data: { items: [appeal()], total: 1, page: 1, pageSize: 50 },
    } as never);
    vi.mocked(reviewAdminAppeal).mockResolvedValue({
      status: 200,
      data: { id: 1, status: 'approved', demoSlug: 'demo-a' },
    } as never);

    renderPage();
    fireEvent.change(await screen.findByLabelText(/裁决说明/), {
      target: { value: '  误判，恢复  ' },
    });
    fireEvent.click(screen.getByRole('button', { name: '通过并恢复' }));

    await waitFor(() =>
      expect(reviewAdminAppeal).toHaveBeenCalledWith(1, {
        decision: 'approve',
        reason: '误判，恢复',
      }),
    );
  });

  test('驳回调用 reject 且不带说明', async () => {
    vi.mocked(listAdminAppeals).mockResolvedValue({
      status: 200,
      data: { items: [appeal()], total: 1, page: 1, pageSize: 50 },
    } as never);
    vi.mocked(reviewAdminAppeal).mockResolvedValue({
      status: 200,
      data: { id: 1, status: 'rejected', demoSlug: 'demo-a' },
    } as never);

    renderPage();
    fireEvent.click(await screen.findByRole('button', { name: '驳回' }));

    await waitFor(() =>
      expect(reviewAdminAppeal).toHaveBeenCalledWith(1, {
        decision: 'reject',
        reason: undefined,
      }),
    );
  });

  test('403 显示没有审核权限', async () => {
    vi.mocked(listAdminAppeals).mockResolvedValue({ status: 403, data: {} } as never);
    renderPage();
    expect(await screen.findByText('没有审核权限')).toBeInTheDocument();
  });

  test('未登录跳登录页', () => {
    auth.me = null;
    renderPage();
    expect(screen.getByText('登录页')).toBeInTheDocument();
    expect(listAdminAppeals).not.toHaveBeenCalled();
  });
});
