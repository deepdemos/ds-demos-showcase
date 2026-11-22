import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { BenchmarksPage } from './BenchmarksPage';

const listBenchmarks = vi.hoisted(() => vi.fn());

vi.mock('@/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/api')>();
  return { ...actual, listBenchmarks };
});

function renderPage() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter>
        <BenchmarksPage />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  listBenchmarks.mockReset();
});

describe('BenchmarksPage', () => {
  it('没有榜时给空态，不给「即将上线」', async () => {
    listBenchmarks.mockResolvedValue({ status: 200, data: { items: [] } });
    renderPage();

    expect(await screen.findByText('还没有接入任何基准榜')).toBeInTheDocument();
  });

  it('按档位如实标注：cite 档显示「仅引用」，不会让人以为缺了分数', async () => {
    listBenchmarks.mockResolvedValue({
      status: 200,
      data: {
        items: [
          {
            slug: 'swe-bench',
            name: 'SWE-bench Verified',
            publisher: 'OpenAI',
            category: 'coding',
            displayTier: 'table',
            latestAsOf: '2026-11-01',
          },
          {
            slug: 'aa-index',
            name: 'Artificial Analysis',
            publisher: '',
            category: '',
            displayTier: 'cite',
            latestAsOf: null,
          },
        ],
      },
    });
    renderPage();

    expect(await screen.findByRole('link', { name: 'SWE-bench Verified' })).toHaveAttribute(
      'href',
      '/benchmarks/swe-bench',
    );
    expect(screen.getByText('有分数')).toBeInTheDocument();
    expect(screen.getByText('仅引用')).toBeInTheDocument();
    // 发布方与方向留空时显示占位，而不是空白单元格。
    expect(screen.getByText('Artificial Analysis')).toBeInTheDocument();
    await waitFor(() => expect(screen.getAllByText('—').length).toBeGreaterThan(0));
  });
});
