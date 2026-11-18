import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router';
import { beforeEach, describe, expect, test, vi } from 'vitest';
import { listClassifications, listDemos } from '@/api';
import { ClassificationPage } from './ClassificationPage';

vi.mock('@/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/api')>();
  return { ...actual, listClassifications: vi.fn(), listDemos: vi.fn() };
});

const catalog = (over: Record<string, unknown> = {}) => ({
  status: 200,
  data: {
    dimensions: [
      { dimension: 'genre', label: '形态', description: '这是什么', mode: 'fixed' },
      { dimension: 'technique', label: '技术', description: '靠什么做的', mode: 'fixed' },
      { dimension: 'subject', label: '题材', description: '对着什么做的', mode: 'open' },
    ],
    items: [
      {
        dimension: 'genre',
        slug: 'game',
        label: '游戏',
        demoCount: 13,
        description: null,
        aliases: [],
        parentSlug: null,
      },
      {
        dimension: 'genre',
        slug: 'puzzle',
        label: '解谜',
        demoCount: 2,
        description: null,
        aliases: [],
        parentSlug: null,
      },
    ],
    ...over,
  },
});

const demo = (slug: string, title: string) => ({
  slug,
  title,
  coverUrl: null,
  createdAt: '2026-01-01T00:00:00Z',
  lifecycleState: 'published',
  matchedIn: 'title',
  classifications: [],
});

const demoList = (items: ReturnType<typeof demo>[], total = items.length) => ({
  status: 200,
  data: { items, total, page: 1, pageSize: 12 },
});

function renderPage(path = '/classifications/genre/game') {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route path="/classifications/:dimension/:slug" element={<ClassificationPage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  vi.mocked(listClassifications).mockReset();
  vi.mocked(listDemos).mockReset();
});

describe('ClassificationPage', () => {
  test('页头显示取值、维度与计数，作品网格链到详情', async () => {
    vi.mocked(listClassifications).mockResolvedValue(catalog() as never);
    vi.mocked(listDemos).mockResolvedValue(
      demoList([demo('demo-a', '一号'), demo('demo-b', '二号')]) as never,
    );

    renderPage();

    expect(await screen.findByRole('heading', { name: '游戏' })).toBeInTheDocument();
    expect(screen.getByText('形态分类')).toBeInTheDocument();
    // 页头与页脚各一个计数；列表回来之后都以 total 为准。
    expect(await screen.findByRole('link', { name: /一号/ })).toHaveAttribute(
      'href',
      '/demos/demo-a',
    );
    expect(screen.getAllByText('共 2 个作品')).toHaveLength(2);
    expect(listDemos).toHaveBeenCalledWith({
      genre: ['game'],
      page: 1,
      pageSize: 12,
      sort: 'latest',
    });
  });

  test('未知 slug / 未知维度都落到「这个分类不存在」，且不发列表请求', async () => {
    vi.mocked(listClassifications).mockResolvedValue(catalog() as never);

    const { unmount } = renderPage('/classifications/genre/not-there');
    expect(await screen.findByText('这个分类不存在')).toBeInTheDocument();
    expect(listDemos).not.toHaveBeenCalled();
    unmount();

    renderPage('/classifications/bogus/game');
    expect(await screen.findByText('这个分类不存在')).toBeInTheDocument();
    expect(listDemos).not.toHaveBeenCalled();
  });

  test('空取值显示空态，不显示空网格', async () => {
    vi.mocked(listClassifications).mockResolvedValue({
      status: 200,
      data: {
        ...catalog().data,
        items: [
          {
            dimension: 'genre',
            slug: 'game',
            label: '游戏',
            demoCount: 0,
            description: null,
            aliases: [],
            parentSlug: null,
          },
        ],
      },
    } as never);
    vi.mocked(listDemos).mockResolvedValue(demoList([]) as never);

    renderPage();

    expect(await screen.findByText('这个分类下还没有作品')).toBeInTheDocument();
  });

  test('翻页与排序会带上对应查询参数', async () => {
    vi.mocked(listClassifications).mockResolvedValue(catalog() as never);
    vi.mocked(listDemos).mockImplementation((params) =>
      Promise.resolve(
        params?.page === 2
          ? (demoList([demo('demo-page2', '第二页作品')], 13) as never)
          : (demoList(
              Array.from({ length: 12 }, (_, index) => demo('demo-' + index, '作品 ' + index)),
              13,
            ) as never),
      ),
    );

    renderPage();

    fireEvent.click(await screen.findByRole('button', { name: '下一页' }));
    await waitFor(() =>
      expect(listDemos).toHaveBeenCalledWith(expect.objectContaining({ page: 2 })),
    );
    expect(await screen.findByRole('link', { name: /第二页作品/ })).toBeInTheDocument();

    fireEvent.change(screen.getByLabelText('排序'), { target: { value: 'hot' } });
    await waitFor(() =>
      expect(listDemos).toHaveBeenCalledWith(expect.objectContaining({ sort: 'hot', page: 1 })),
    );
  });

  test('同一维度下的其他取值给横向入口', async () => {
    vi.mocked(listClassifications).mockResolvedValue(catalog() as never);
    vi.mocked(listDemos).mockResolvedValue(demoList([demo('demo-a', '一号')]) as never);

    renderPage();

    expect(await screen.findByRole('link', { name: /解谜/ })).toHaveAttribute(
      'href',
      '/classifications/genre/puzzle',
    );
  });
});
