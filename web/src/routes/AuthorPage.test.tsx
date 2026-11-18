import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router';
import { beforeEach, describe, expect, test, vi } from 'vitest';
import { getUserProfile, listUserDemos } from '@/api';
import { AuthorPage } from './AuthorPage';

vi.mock('@/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/api')>();
  return { ...actual, getUserProfile: vi.fn(), listUserDemos: vi.fn() };
});

const profile = (over: Record<string, unknown> = {}) => ({
  status: 200,
  data: {
    username: 'alice',
    displayName: 'Alice',
    bio: '做点小东西',
    avatarUrl: null,
    joinedAt: '2026-01-01T00:00:00Z',
    demoCount: 2,
    ...over,
  },
});

const demo = (slug: string, title: string) => ({
  slug,
  title,
  coverUrl: null,
  createdAt: '2026-01-01T00:00:00Z',
});

const demoList = (items: ReturnType<typeof demo>[], total = items.length) => ({
  status: 200,
  data: { items, total, page: 1, pageSize: 12 },
});

function renderPage(path = '/u/alice') {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route path="/u/:username" element={<AuthorPage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  vi.mocked(getUserProfile).mockReset();
  vi.mocked(listUserDemos).mockReset();
});

describe('AuthorPage', () => {
  test('显示公开资料与公开作品，作品链到详情', async () => {
    vi.mocked(getUserProfile).mockResolvedValue(profile() as never);
    vi.mocked(listUserDemos).mockResolvedValue(
      demoList([demo('demo-a', '一号'), demo('demo-b', '二号')]) as never,
    );

    renderPage();

    expect(await screen.findByRole('heading', { name: 'Alice' })).toBeInTheDocument();
    expect(screen.getByText('@alice')).toBeInTheDocument();
    expect(screen.getByText('做点小东西')).toBeInTheDocument();
    expect(screen.getByText(/公开作品 2 件/)).toBeInTheDocument();
    expect(await screen.findByRole('link', { name: /一号/ })).toHaveAttribute(
      'href',
      '/demos/demo-a',
    );
    expect(listUserDemos).toHaveBeenCalledWith('alice', { page: 1, pageSize: 12 });
  });

  test('用户名不存在时显示「没有这个作者」，不请求作品列表', async () => {
    vi.mocked(getUserProfile).mockResolvedValue({
      status: 404,
      data: { code: 'not_found', message: '作者不存在' },
    } as never);

    renderPage('/u/nobody');

    expect(await screen.findByText('没有这个作者')).toBeInTheDocument();
    expect(listUserDemos).not.toHaveBeenCalled();
  });

  test('作者存在但没有公开作品时显示空态', async () => {
    vi.mocked(getUserProfile).mockResolvedValue(profile({ demoCount: 0 }) as never);
    vi.mocked(listUserDemos).mockResolvedValue(demoList([]) as never);

    renderPage();

    expect(await screen.findByText('还没有公开作品')).toBeInTheDocument();
  });

  test('有头像时渲染头像图，没有时不请求第三方占位', async () => {
    vi.mocked(getUserProfile).mockResolvedValue(
      profile({ avatarUrl: 'https://cdn.example/avatar.png' }) as never,
    );
    vi.mocked(listUserDemos).mockResolvedValue(demoList([]) as never);

    const { container } = renderPage();

    await screen.findByRole('heading', { name: 'Alice' });
    const img = container.querySelector('img[src="https://cdn.example/avatar.png"]');
    expect(img).not.toBeNull();
  });

  test('翻页后带 page 参数重新请求', async () => {
    vi.mocked(getUserProfile).mockResolvedValue(profile({ demoCount: 13 }) as never);
    vi.mocked(listUserDemos).mockImplementation((_username, params) =>
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
      expect(listUserDemos).toHaveBeenCalledWith('alice', { page: 2, pageSize: 12 }),
    );
    expect(await screen.findByRole('link', { name: /第二页作品/ })).toBeInTheDocument();
  });
});
