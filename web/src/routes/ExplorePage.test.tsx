import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { expect, test, vi } from 'vitest';
import { listDemos } from '@/api';
import { ExplorePage } from './ExplorePage';

vi.mock('@/api', () => ({ listDemos: vi.fn() }));

function result(slug: string, title: string) {
  return {
    status: 200,
    data: {
      items: [
        {
          slug,
          title,
          coverUrl: null,
          createdAt: '2026-01-01T00:00:00Z',
          matchedIn: 'title',
        },
      ],
      page: 1,
      pageSize: 6,
      total: 1,
    },
  } as never;
}

test('探索页同时给出热门与最新作品', async () => {
  vi.mocked(listDemos).mockImplementation(async (params) =>
    params?.sort === 'hot' ? result('hot-demo', '热门作品') : result('new-demo', '最新作品'),
  );

  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter>
        <ExplorePage />
      </MemoryRouter>
    </QueryClientProvider>,
  );

  expect(await screen.findByRole('heading', { name: '正在热门' })).toBeInTheDocument();
  expect(await screen.findByRole('link', { name: /热门作品/ })).toHaveAttribute(
    'href',
    '/demos/hot-demo',
  );
  expect(await screen.findByRole('link', { name: /最新作品/ })).toHaveAttribute(
    'href',
    '/demos/new-demo',
  );
});
