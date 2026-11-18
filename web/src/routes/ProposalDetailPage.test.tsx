import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router';
import { beforeEach, describe, expect, test, vi } from 'vitest';
import { getAdminProposal, reviewAdminProposal } from '@/api';
import { ProposalDetailPage } from './ProposalDetailPage';

const auth = vi.hoisted(() => ({
  me: null as null | { sub: string; username: string },
}));

vi.mock('@/features/auth/AuthContext', () => ({
  useAuth: () => ({ ...auth, isLoading: false, isError: false, setMe: vi.fn(), signOut: vi.fn() }),
}));

vi.mock('@/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/api')>();
  return { ...actual, getAdminProposal: vi.fn(), reviewAdminProposal: vi.fn() };
});

const detail = (over: Record<string, unknown> = {}) => ({
  id: 7,
  kind: 'new_task',
  status: 'pending',
  payload: { title: '提案标题', brief: '一句话说明' },
  source: 'user',
  confidence: null,
  createdBy: 1,
  createdAt: '2026-11-18T00:00:00Z',
  reviewedBy: null,
  reviewedAt: null,
  rejectReason: null,
  resultKind: null,
  resultId: null,
  updatedAt: '2026-11-18T00:00:00Z',
  ...over,
});

function renderPage() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={['/admin/proposals/7']}>
        <Routes>
          <Route path="/admin/proposals/:id" element={<ProposalDetailPage />} />
          <Route path="/login" element={<div>登录页</div>} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  auth.me = { sub: 'u1', username: 'alice' };
  vi.mocked(getAdminProposal).mockReset();
  vi.mocked(reviewAdminProposal).mockReset();
});

describe('ProposalDetailPage', () => {
  test('渲染 payload、来源与状态，通过按钮不可用', async () => {
    vi.mocked(getAdminProposal).mockResolvedValue({ status: 200, data: detail() } as never);

    renderPage();

    expect(await screen.findByRole('heading', { name: '提案标题' })).toBeInTheDocument();
    expect(screen.getByText(/new_task · user · 待处理/)).toBeInTheDocument();
    expect(screen.getByText(/一句话说明/)).toBeInTheDocument();
    expect(screen.getByText('通过（执行器未接入）')).toBeInTheDocument();
  });

  test('驳回会调接口并带上原因', async () => {
    vi.mocked(getAdminProposal).mockResolvedValue({ status: 200, data: detail() } as never);
    vi.mocked(reviewAdminProposal).mockResolvedValue({
      status: 200,
      data: { id: 7, status: 'rejected' },
    } as never);

    renderPage();
    fireEvent.change(await screen.findByLabelText('驳回原因（可选）'), {
      target: { value: '  重复提案  ' },
    });
    fireEvent.click(screen.getByRole('button', { name: '驳回' }));

    await waitFor(() =>
      expect(reviewAdminProposal).toHaveBeenCalledWith(7, {
        decision: 'reject',
        reason: '重复提案',
      }),
    );
  });

  test('已驳回显示原因且不再给裁决按钮', async () => {
    auth.me = { sub: 'u1', username: 'alice' };
    vi.mocked(getAdminProposal).mockResolvedValue({
      status: 200,
      data: detail({ status: 'rejected', rejectReason: '封面不合格' }),
    } as never);

    renderPage();

    expect(await screen.findByText('驳回原因：封面不合格')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: '驳回' })).toBeNull();
  });

  test('无权限与未登录', async () => {
    vi.mocked(getAdminProposal).mockResolvedValue({
      status: 403,
      data: { code: 'forbidden', message: '没有权限' },
    } as never);
    const { unmount } = renderPage();
    expect(await screen.findByText('没有提案审核权限')).toBeInTheDocument();
    unmount();

    auth.me = null;
    renderPage();
    expect(screen.getByText('登录页')).toBeInTheDocument();
    expect(getAdminProposal).toHaveBeenCalledTimes(1);
  });
});
