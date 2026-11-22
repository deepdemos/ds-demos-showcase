import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { beforeEach, expect, test, vi } from 'vitest';
import { listDemos } from '@/api';
import { SearchPage } from './SearchPage';

vi.mock('@/api', () => ({ listDemos: vi.fn() }));

function page(items: Array<{ slug: string; title: string }>, total = items.length) {
  return {
    status: 200,
    data: {
      items: items.map((item) => ({
        ...item,
        coverUrl: null,
        createdAt: '2026-01-01T00:00:00Z',
      })),
      page: 1,
      pageSize: 20,
      total,
    },
  } as never;
}

beforeEach(() => {
  vi.mocked(listDemos).mockReset();
  vi.mocked(listDemos).mockResolvedValue(page([]));
});

function renderAt(path: string) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[path]}>
        <SearchPage />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

test('把关键词带给列表接口并显示结果', async () => {
  vi.mocked(listDemos).mockResolvedValue(page([{ slug: 'demo-a', title: '钢琴卷帘' }]));
  renderAt('/search?q=钢琴');
  expect(await screen.findByRole('link', { name: /钢琴卷帘/ })).toHaveAttribute(
    'href',
    '/demos/demo-a',
  );
  expect(listDemos).toHaveBeenCalledWith(expect.objectContaining({ q: '钢琴', page: 1 }));
});

test('空关键词显示输入提示，列表走空查询', async () => {
  renderAt('/search');
  expect(await screen.findByText('还没有搜索')).toBeInTheDocument();
  expect(listDemos).toHaveBeenCalled();
});

test('提交搜索词后更新 URL', async () => {
  vi.mocked(listDemos).mockResolvedValue(page([]));
  renderAt('/search');
  await screen.findByText('还没有搜索');
  fireEvent.change(screen.getByLabelText('搜索作品'), { target: { value: '探针' } });
  fireEvent.click(screen.getByRole('button', { name: '搜索' }));
  await waitFor(() =>
    expect(listDemos).toHaveBeenLastCalledWith(expect.objectContaining({ q: '探针' })),
  );
});
