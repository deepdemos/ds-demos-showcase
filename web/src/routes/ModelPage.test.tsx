import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { ModelPage } from './ModelPage';

const getModel = vi.hoisted(() => vi.fn());

const auth = vi.hoisted(() => ({ me: null as { role: string } | null }));

vi.mock('@/features/auth/AuthContext', () => ({
  useAuth: () => ({ me: auth.me, isLoading: false }),
}));

vi.mock('@/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/api')>();
  return { ...actual, getModel };
});

/** 一个「有档案、有票、有基准」的完整模型页。 */
function profile(overrides: Record<string, unknown> = {}) {
  return {
    model: {
      slug: 'deepseek-chat',
      name: 'DeepSeek Chat',
      vendor: 'DeepSeek',
      status: 'active',
      resolution: 'exact',
      mergedIntoSlug: '',
      source: 'manual',
      aliases: [],
      description: '对话模型',
    },
    specs: {
      contextWindow: 131072,
      maxOutput: 8192,
      paramTotalB: null,
      paramActiveB: null,
      paramKind: null,
      releaseDate: '2026-01-15',
      knowledgeCutoff: null,
      openWeights: 0,
      costIn: null,
      costOut: null,
      license: null,
      modalitiesIn: 'text',
      modalitiesOut: 'text',
      source: 'manual',
      note: null,
    },
    site: { demoCount: 2, votes: 3, scored: true, score: 4.25, priorWeight: 2 },
    demos: [
      {
        slug: 'demo-a',
        title: '钢琴卷帘',
        createdAt: '2026-02-01T00:00:00+08:00',
        ratingAverage: 4.5,
        ratingCount: 2,
      },
    ],
    providers: [
      {
        providerId: null,
        providerSlug: '',
        providerName: '',
        demoCount: 2,
        votes: 3,
        avgRating: 4.25,
      },
    ],
    benchmarks: [],
    ...overrides,
  };
}

function renderPage(slug = 'deepseek-chat') {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[`/models/${slug}`]}>
        <Routes>
          <Route path="/models/:slug" element={<ModelPage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('模型页', () => {
  beforeEach(() => {
    getModel.mockReset();
    auth.me = null;
  });

  it('显示档案、站内表现与作品链接', async () => {
    getModel.mockResolvedValue({ status: 200, data: profile() });
    renderPage();

    await waitFor(() => expect(screen.getByText('DeepSeek Chat')).toBeInTheDocument());
    expect(screen.getByText('131,072 tokens')).toBeInTheDocument();
    expect(screen.getByText('2 件')).toBeInTheDocument();
    expect(screen.getByText('4.25')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: '钢琴卷帘' })).toHaveAttribute('href', '/demos/demo-a');
  });

  it('页面顶部给出回作品库的出口', async () => {
    getModel.mockResolvedValue({ status: 200, data: profile() });
    renderPage();

    await waitFor(() => expect(screen.getByText('DeepSeek Chat')).toBeInTheDocument());
    expect(screen.getByRole('link', { name: '← 返回作品库' })).toHaveAttribute('href', '/demos');
  });

  it('作品数链到作品库的模型筛选，而不是一个死数字', async () => {
    getModel.mockResolvedValue({ status: 200, data: profile() });
    renderPage();

    await waitFor(() => expect(screen.getByText('DeepSeek Chat')).toBeInTheDocument());
    expect(screen.getByRole('link', { name: '2 件' })).toHaveAttribute(
      'href',
      '/demos?model=deepseek-chat',
    );
  });

  it('没有公开作品时作品数保持纯文本，不给空列表入口', async () => {
    getModel.mockResolvedValue({
      status: 200,
      data: profile({ site: { demoCount: 0, votes: 0, scored: false, score: 0, priorWeight: 1 } }),
    });
    renderPage();

    await waitFor(() => expect(screen.getByText('0 件')).toBeInTheDocument());
    // 关键断言：0 件时不能变成链接——点进去只会看到「没有匹配的作品」。
    expect(screen.queryByRole('link', { name: '0 件' })).not.toBeInTheDocument();
  });

  it('没有票时说「还没有人评分」，不显示 0 分', async () => {
    getModel.mockResolvedValue({
      status: 200,
      data: profile({ site: { demoCount: 1, votes: 0, scored: false, score: 0, priorWeight: 1 } }),
    });
    renderPage();

    await waitFor(() => expect(screen.getByText('还没有人评分')).toBeInTheDocument());
    // 关键断言：不能出现一个看起来像真数据的 0.00。
    expect(screen.queryByText('0.00')).not.toBeInTheDocument();
  });

  it('没有档案时说「还没有档案」，而不是排一排 0', async () => {
    getModel.mockResolvedValue({ status: 200, data: profile({ specs: null }) });
    renderPage();

    await waitFor(() => expect(screen.getByText(/还没有这个模型的档案/)).toBeInTheDocument());
  });

  it('没有基准时说「还没有接入任何基准榜」', async () => {
    getModel.mockResolvedValue({ status: 200, data: profile() });
    renderPage();

    await waitFor(() => expect(screen.getByText(/还没有接入任何基准榜/)).toBeInTheDocument());
  });

  it('404 时显示「没有这个模型」并给出一条真能走的出口', async () => {
    getModel.mockResolvedValue({ status: 404, data: undefined });
    renderPage('nope');

    await waitFor(() => expect(screen.getByText('没有这个模型')).toBeInTheDocument());
    // 出口是作品库：模型目录页还没做，指向 `/models` 会 404。
    expect(screen.getByRole('link', { name: '去作品库' })).toHaveAttribute('href', '/demos');
  });

  it('接口失败时显示错误并允许重试', async () => {
    getModel.mockResolvedValue({ status: 503, data: undefined });
    renderPage();

    await waitFor(() => expect(screen.getByRole('alert')).toBeInTheDocument());
    expect(screen.getByRole('button', { name: '重试' })).toBeInTheDocument();
  });

  it('合并到别的模型时给出归宿链接', async () => {
    const merged = profile();
    merged.model.status = 'deprecated';
    merged.model.mergedIntoSlug = 'deepseek-chat-v2';
    getModel.mockResolvedValue({ status: 200, data: merged });
    renderPage();

    await waitFor(() =>
      expect(screen.getByRole('link', { name: 'deepseek-chat-v2' })).toHaveAttribute(
        'href',
        '/models/deepseek-chat-v2',
      ),
    );
  });

  // 档案为空时原本文案是「管理员可以从导入页补录」：管理员读了还得手敲地址，
  // 普通用户读到的是一句他自己无从来执行的指示（issue #374）。
  it('档案为空时，管理员拿到带模型名的导入页链接', async () => {
    auth.me = { role: 'admin' };
    getModel.mockResolvedValue({ status: 200, data: profile({ specs: null }) });
    renderPage();

    await waitFor(() => expect(screen.getByText('DeepSeek Chat')).toBeInTheDocument());
    expect(screen.getByRole('link', { name: '导入页' })).toHaveAttribute(
      'href',
      '/admin/models?q=deepseek-chat',
    );
    expect(screen.queryByText(/管理员可以从导入页补录/)).toBeNull();
  });

  it('档案为空时，普通访客不看到管理员才有的指示', async () => {
    getModel.mockResolvedValue({ status: 200, data: profile({ specs: null }) });
    renderPage();

    await waitFor(() => expect(screen.getByText(/等它被收录/)).toBeInTheDocument());
    expect(screen.queryByRole('link', { name: '导入页' })).toBeNull();
    expect(screen.queryByText(/管理员可以从导入页补录/)).toBeNull();
  });
});
