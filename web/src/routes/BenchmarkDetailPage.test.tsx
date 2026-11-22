import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { BenchmarkDetailPage } from './BenchmarkDetailPage';

const getBenchmark = vi.hoisted(() => vi.fn());

vi.mock('@/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/api')>();
  return { ...actual, getBenchmark };
});

function renderPage(slug = 'swe-bench') {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={['/benchmarks/' + slug]}>
        <Routes>
          <Route path="/benchmarks/:slug" element={<BenchmarkDetailPage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

function detail(overrides: Record<string, unknown> = {}) {
  return {
    slug: 'swe-bench',
    name: 'SWE-bench Verified',
    publisher: 'OpenAI',
    homepageUrl: 'https://www.swebench.com/',
    description: '真实 GitHub issue 的修复率',
    category: 'coding',
    displayTier: 'table',
    license: 'CC-BY',
    licenseUrl: '',
    attribution: 'OpenAI',
    versions: [
      {
        version: 'v2',
        asOf: '2026-11-01',
        sourceType: 'leaderboard',
        coverage: 'full',
        participantCount: 12,
        sourceUrl: '',
      },
      {
        version: 'self-1',
        asOf: '2026-10-01',
        sourceType: 'self_reported',
        coverage: 'partial',
        participantCount: 3,
        sourceUrl: '',
      },
    ],
    metrics: [
      {
        key: 'score',
        label: '得分',
        unit: '%',
        isPrimary: true,
        higherIsBetter: true,
        scores: [
          {
            modelSlug: 'deepseek-chat',
            modelName: 'DeepSeek Chat',
            externalName: 'deepseek-chat',
            rawScore: 42.5,
            rank: 1,
            sampleSize: 200,
            uncertaintyLower: 40,
            uncertaintyUpper: 45,
            matchStatus: 'matched',
          },
        ],
      },
    ],
    ...overrides,
  };
}

beforeEach(() => {
  getBenchmark.mockReset();
});

describe('BenchmarkDetailPage', () => {
  it('404 时给「不存在或已下架」，并留回目录的入口', async () => {
    getBenchmark.mockResolvedValue({ status: 404, data: { code: 'not_found', message: '没了' } });
    renderPage();

    expect(await screen.findByText('这张基准不存在或已下架')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: '回基准目录' })).toHaveAttribute('href', '/benchmarks');
  });

  it('指标表给分数与区间；厂商自报只出现在版本历史', async () => {
    getBenchmark.mockResolvedValue({ status: 200, data: detail() });
    renderPage();

    expect(await screen.findByRole('link', { name: 'DeepSeek Chat' })).toHaveAttribute(
      'href',
      '/models/deepseek-chat',
    );
    expect(screen.getByText('42.5')).toBeInTheDocument();
    expect(screen.getByText('40 – 45')).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: '厂商自报' })).toBeInTheDocument();
    // 自报版本出现在版本历史，但**不在**指标表的那一行里（没有它的分数）。
    expect(screen.getByText(/self-1/)).toBeInTheDocument();
  });
});
