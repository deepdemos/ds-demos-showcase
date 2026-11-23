import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { BenchmarkOverviewPage } from './BenchmarkOverviewPage';

const getBenchmarkOverview = vi.hoisted(() => vi.fn());

vi.mock('@/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/api')>();
  return { ...actual, getBenchmarkOverview };
});

const overview = {
  columns: [
    { benchmarkSlug: 'humaneval', name: 'HumanEval', unit: '%', version: 'v1' },
    { benchmarkSlug: 'mmlu', name: 'MMLU', unit: '%', version: '2026-11' },
  ],
  rows: [
    {
      modelSlug: 'deepseek-coder',
      modelName: 'DeepSeek Coder',
      cells: [{ benchmarkSlug: 'humaneval', rawScore: 86.4, rank: 1 }],
    },
    {
      modelSlug: 'deepseek-reasoner',
      modelName: 'DeepSeek Reasoner',
      cells: [
        { benchmarkSlug: 'humaneval', rawScore: 84.1, rank: null },
        { benchmarkSlug: 'mmlu', rawScore: 81.2, rank: 1 },
      ],
    },
  ],
};

function renderPage() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter>
        <BenchmarkOverviewPage />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  getBenchmarkOverview.mockReset();
});

describe('BenchmarkOverviewPage', () => {
  it('一行一个模型、一列一张榜，格子给原始分与名次', async () => {
    getBenchmarkOverview.mockResolvedValue({ status: 200, data: overview });
    renderPage();

    expect(await screen.findByRole('rowheader', { name: 'DeepSeek Coder' })).toBeInTheDocument();
    expect(screen.getByRole('rowheader', { name: 'DeepSeek Reasoner' })).toBeInTheDocument();

    // 列头：榜名 + 单位 · 版本；点进去是该榜详情。
    const header = screen.getByRole('link', { name: 'HumanEval' });
    expect(header).toHaveAttribute('href', '/benchmarks/humaneval');
    expect(screen.getByRole('columnheader', { name: /HumanEval/ })).toHaveTextContent('% · v1');

    const row = screen.getByRole('rowheader', { name: 'DeepSeek Coder' }).closest('tr');
    const cells = within(row as HTMLElement).getAllByRole('cell');
    expect(cells[0]).toHaveTextContent('86.4');
    expect(cells[0]).toHaveTextContent('#1');
  });

  it('缺格显示「—」，不补零', async () => {
    getBenchmarkOverview.mockResolvedValue({ status: 200, data: overview });
    renderPage();

    // DeepSeek Coder 在 MMLU 一列没有分数：第二列（cells[1]）是缺格。
    const row = (await screen.findByRole('rowheader', { name: 'DeepSeek Coder' })).closest('tr');
    const cells = within(row as HTMLElement).getAllByRole('cell');
    expect(cells[1]).toHaveTextContent('—');
    expect(cells[1]).not.toHaveTextContent('0');
  });

  it('源榜没给名次时只显示分数，不编一个名次', async () => {
    getBenchmarkOverview.mockResolvedValue({ status: 200, data: overview });
    renderPage();

    const row = (await screen.findByRole('rowheader', { name: 'DeepSeek Reasoner' })).closest('tr');
    const cells = within(row as HTMLElement).getAllByRole('cell');
    expect(cells[0]).toHaveTextContent('84.1');
    expect(cells[0]).not.toHaveTextContent('#');
  });

  it('一列都没有时给空态，不白屏也不编总分', async () => {
    getBenchmarkOverview.mockResolvedValue({ status: 200, data: { columns: [], rows: [] } });
    renderPage();

    expect(await screen.findByText('还没有能放进矩阵的分数')).toBeInTheDocument();
    expect(screen.queryByText(/综合分|总分/)).toBeNull();
  });
});
