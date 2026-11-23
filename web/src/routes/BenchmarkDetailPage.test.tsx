import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, within } from '@testing-library/react';
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

  it('cite 档不渲染指标表，写清「不收录」的缘由、外链与署名', async () => {
    getBenchmark.mockResolvedValue({
      status: 200,
      data: detail({ displayTier: 'cite', metrics: [] }),
    });
    renderPage();

    expect(await screen.findByText('本站不收录这张榜的分数')).toBeInTheDocument();
    // 关键：一张空表会被读成「我们缺数据」，所以整块指标区都不出现。
    expect(screen.queryByRole('heading', { name: '指标' })).toBeNull();
    expect(screen.queryByRole('table')).toBeNull();
    const notice = screen.getByText('本站不收录这张榜的分数').closest('section') as HTMLElement;
    const official = within(notice).getByRole('link', { name: 'https://www.swebench.com/' });
    expect(official).toHaveAttribute('rel', 'noreferrer noopener');
    expect(official).toHaveAttribute('target', '_blank');
    expect(screen.getByText(/引用要求署名：OpenAI/)).toBeInTheDocument();
    expect(screen.queryByText(/即将上线|综合分/)).toBeNull();
  });

  it('link 档只留概述与外链，版本历史与指标都不出现', async () => {
    getBenchmark.mockResolvedValue({
      status: 200,
      data: detail({ displayTier: 'link', metrics: [] }),
    });
    renderPage();

    expect(await screen.findByText('站内只登记出处')).toBeInTheDocument();
    expect(screen.queryByRole('heading', { name: '版本历史' })).toBeNull();
    expect(screen.queryByRole('heading', { name: '指标' })).toBeNull();
    // 连版本号都不该出现：这张榜我们没有可陈述的口径。
    expect(screen.queryByText(/v2/)).toBeNull();
  });
});
