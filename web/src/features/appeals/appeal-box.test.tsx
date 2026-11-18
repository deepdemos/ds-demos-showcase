import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { beforeEach, describe, expect, test, vi } from 'vitest';
import { getDemoAppeal, submitDemoAppeal } from '@/api';
import { AppealBox } from './appeal-box';

const auth = vi.hoisted(() => ({
  me: null as null | { sub: string; username: string },
}));

vi.mock('@/features/auth/AuthContext', () => ({
  useAuth: () => ({ ...auth, isLoading: false, isError: false, setMe: vi.fn(), signOut: vi.fn() }),
}));

vi.mock('@/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/api')>();
  return { ...actual, getDemoAppeal: vi.fn(), submitDemoAppeal: vi.fn() };
});

function renderBox() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter>
        <AppealBox slug="demo-x" />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  auth.me = null;
  vi.mocked(getDemoAppeal).mockReset();
  vi.mocked(submitDemoAppeal).mockReset();
});

describe('AppealBox', () => {
  test('未登录：只给登录入口与社区规范', () => {
    renderBox();
    expect(screen.getByText('查看社区规范')).toHaveAttribute('href', '/community-guidelines');
    expect(screen.getByText('去登录')).toHaveAttribute(
      'href',
      '/login?return_to=%2Fdemos%2Fdemo-x',
    );
    expect(getDemoAppeal).not.toHaveBeenCalled();
  });

  test('可申诉时提交，成功后清空输入', async () => {
    auth.me = { sub: 'u1', username: 'alice' };
    vi.mocked(getDemoAppeal).mockResolvedValue({
      status: 200,
      data: { canSubmit: true, appeal: null },
    } as never);
    vi.mocked(submitDemoAppeal).mockResolvedValue({
      status: 201,
      data: {
        id: 1,
        reason: '误判',
        status: 'pending',
        decisionReason: null,
        reviewedAt: null,
        createdAt: '2026-11-18T00:00:00Z',
      },
    } as never);

    renderBox();
    const input = await screen.findByLabelText('申诉说明（1–2000 字）');
    fireEvent.change(input, { target: { value: '  误判  ' } });
    fireEvent.click(screen.getByRole('button', { name: '提交申诉' }));

    await waitFor(() =>
      expect(submitDemoAppeal).toHaveBeenCalledWith('demo-x', { reason: '误判' }),
    );
  });

  test('处理中显示状态且不再给提交按钮', async () => {
    auth.me = { sub: 'u1', username: 'alice' };
    vi.mocked(getDemoAppeal).mockResolvedValue({
      status: 200,
      data: {
        canSubmit: false,
        appeal: {
          id: 1,
          reason: '误判',
          status: 'pending',
          decisionReason: null,
          reviewedAt: null,
          createdAt: '2026-11-18T00:00:00Z',
        },
      },
    } as never);

    renderBox();
    expect(await screen.findByText(/申诉处理中/)).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: '提交申诉' })).toBeNull();
  });

  test('非作者只看到社区规范', async () => {
    auth.me = { sub: 'u1', username: 'alice' };
    vi.mocked(getDemoAppeal).mockResolvedValue({ status: 404, data: {} } as never);

    renderBox();
    await waitFor(() => expect(getDemoAppeal).toHaveBeenCalled());
    expect(screen.queryByText(/申诉/)).toBeNull();
    expect(screen.getByText('查看社区规范')).toBeInTheDocument();
  });
});
