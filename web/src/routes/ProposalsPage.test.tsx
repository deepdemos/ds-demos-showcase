import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { expect, test, vi } from 'vitest';
import { listAdminProposals } from '@/api';
import { ProposalsPage } from './ProposalsPage';

vi.mock('@/api', () => ({ listAdminProposals: vi.fn() }));

test('提案列表显示状态与详情链接', async () => {
  vi.mocked(listAdminProposals).mockResolvedValue({
    status: 200,
    data: {
      items: [
        {
          id: 7,
          kind: 'new_benchmark',
          status: 'pending',
          source: 'user',
          title: '新基准提案',
          createdAt: '2026-01-01T00:00:00Z',
          updatedAt: '2026-01-01T00:00:00Z',
        },
      ],
      page: 1,
      pageSize: 20,
      total: 1,
    },
  } as never);

  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter>
        <ProposalsPage />
      </MemoryRouter>
    </QueryClientProvider>,
  );

  expect(await screen.findByRole('link', { name: '新基准提案' })).toHaveAttribute(
    'href',
    '/admin/proposals/7',
  );
  expect(screen.getByText(/new_benchmark/)).toBeInTheDocument();
});
