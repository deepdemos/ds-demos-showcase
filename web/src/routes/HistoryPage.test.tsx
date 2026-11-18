import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { beforeEach, describe, expect, test, vi } from 'vitest';
import { deleteMyHistory, getMyHistory, updateMyHistorySettings } from '@/api';
import { HistoryPage } from './HistoryPage';
import {
  HISTORY_STORAGE_KEY,
  readHistory,
  type HistoryEntry,
} from '@/features/history/local-history';

const auth = vi.hoisted(() => ({
  me: null as null | { sub: string; username: string; displayName: string },
}));

vi.mock('@/features/auth/AuthContext', () => ({
  useAuth: () => ({ ...auth, isLoading: false, isError: false, setMe: vi.fn(), signOut: vi.fn() }),
}));

vi.mock('@/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/api')>();
  return {
    ...actual,
    getMyHistory: vi.fn(),
    deleteMyHistory: vi.fn(),
    updateMyHistorySettings: vi.fn(),
  };
});

const localEntry = (slug: string, title: string, at: string): HistoryEntry => ({
  slug,
  title,
  coverUrl: null,
  viewedAt: at,
});

function seedLocal(items: HistoryEntry[]) {
  localStorage.setItem(HISTORY_STORAGE_KEY, JSON.stringify(items));
}

const accountItem = (slug: string, title: string, at: string) => ({
  slug,
  title,
  coverUrl: null,
  viewedAt: at,
});

function accountHistory(
  items: ReturnType<typeof accountItem>[],
  over: Record<string, unknown> = {},
) {
  return {
    status: 200,
    data: {
      enabled: true,
      items,
      total: items.length,
      page: 1,
      pageSize: 100,
      ...over,
    },
  };
}

function renderPage() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter>
        <HistoryPage />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  localStorage.clear();
  auth.me = null;
  vi.mocked(getMyHistory).mockReset();
  vi.mocked(deleteMyHistory).mockReset();
  vi.mocked(updateMyHistorySettings).mockReset();
});

describe('HistoryPage', () => {
  test('未登录只看本机记录，并给登录入口', async () => {
    seedLocal([
      localEntry('demo-a', '一号作品', '2026-11-02T00:00:00.000Z'),
      localEntry('demo-b', '二号作品', '2026-11-01T00:00:00.000Z'),
    ]);
    renderPage();

    expect(screen.getByRole('link', { name: '一号作品' })).toHaveAttribute('href', '/demos/demo-a');
    expect(screen.getByText('去登录')).toHaveAttribute('href', '/login?return_to=/me/history');
    expect(getMyHistory).not.toHaveBeenCalled();
    expect(screen.getAllByText(/本机 · /).length).toBeGreaterThanOrEqual(1);
  });

  test('登录后合并两个来源：按较近一次去重并标来源', async () => {
    auth.me = { sub: 'u1', username: 'alice', displayName: 'Alice' };
    seedLocal([
      localEntry('demo-a', '本机一号', '2026-11-01T00:00:00.000Z'),
      localEntry('demo-c', '本机三号', '2026-11-05T00:00:00.000Z'),
      localEntry('demo-d', '本机四号', '2026-11-03T00:00:00.000Z'),
    ]);
    vi.mocked(getMyHistory).mockResolvedValue(
      accountHistory([
        accountItem('demo-a', '账号一号', '2026-11-04T00:00:00.000Z'),
        accountItem('demo-b', '账号二号', '2026-11-02T00:00:00.000Z'),
        accountItem('demo-c', '账号三号', '2026-11-04T00:00:00.000Z'),
      ]) as never,
    );

    const { container } = renderPage();

    await waitFor(() => expect(screen.getByText(/共 4 条/)).toBeInTheDocument());
    const row = (slug: string) => container.querySelector('[data-slug="' + slug + '"]');
    expect(row('demo-a')?.getAttribute('data-source')).toBe('account');
    expect(row('demo-b')?.getAttribute('data-source')).toBe('account');
    expect(row('demo-c')?.getAttribute('data-source')).toBe('local');
    expect(row('demo-d')?.getAttribute('data-source')).toBe('local');
    // 时间近的那条提供标题：a 用账号侧，c 用本机侧。
    expect(screen.getByRole('link', { name: '账号一号' })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: '本机三号' })).toBeInTheDocument();
    expect(screen.getAllByText(/本机 · /).length).toBeGreaterThanOrEqual(1);
    expect(screen.getAllByText(/账号 · /).length).toBeGreaterThanOrEqual(1);
  });

  test('删除本机记录后，账号侧同一条仍然在', async () => {
    auth.me = { sub: 'u1', username: 'alice', displayName: 'Alice' };
    seedLocal([localEntry('demo-a', '本机一号', '2026-11-01T00:00:00.000Z')]);
    vi.mocked(getMyHistory).mockResolvedValue(
      accountHistory([accountItem('demo-a', '账号一号', '2026-11-04T00:00:00.000Z')]) as never,
    );

    const { container } = renderPage();
    fireEvent.click(await screen.findByRole('button', { name: '删除本机记录 账号一号' }));

    expect(readHistory()).toEqual([]);
    await waitFor(() =>
      expect(container.querySelector('[data-slug="demo-a"]')?.getAttribute('data-source')).toBe(
        'account',
      ),
    );
  });

  test('关闭账号历史会写设置，且不清空已有记录', async () => {
    auth.me = { sub: 'u1', username: 'alice', displayName: 'Alice' };
    vi.mocked(getMyHistory).mockResolvedValue(
      accountHistory([accountItem('demo-a', '账号一号', '2026-11-04T00:00:00.000Z')]) as never,
    );
    vi.mocked(updateMyHistorySettings).mockResolvedValue({
      status: 200,
      data: { enabled: false },
    } as never);

    renderPage();
    fireEvent.click(await screen.findByRole('checkbox', { name: '记录账号历史' }));

    await waitFor(() => expect(updateMyHistorySettings).toHaveBeenCalledWith({ enabled: false }));
    expect(deleteMyHistory).not.toHaveBeenCalled();
  });

  test('清空账号历史要先看到确认，再调 DELETE', async () => {
    auth.me = { sub: 'u1', username: 'alice', displayName: 'Alice' };
    vi.mocked(getMyHistory).mockResolvedValue(
      accountHistory([
        accountItem('demo-a', '账号一号', '2026-11-04T00:00:00.000Z'),
        accountItem('demo-b', '账号二号', '2026-11-03T00:00:00.000Z'),
      ]) as never,
    );
    vi.mocked(deleteMyHistory).mockResolvedValue({ status: 204, data: undefined } as never);

    renderPage();
    const clearButton = await screen.findByRole('button', { name: '清空账号历史' });
    // 账号历史加载完成前按钮是 disabled，先等它可用再点。
    await waitFor(() => expect(clearButton).toBeEnabled());
    fireEvent.click(clearButton);

    expect(await screen.findByText(/会删除全部账号浏览记录（2 条）/)).toBeInTheDocument();
    expect(deleteMyHistory).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole('button', { name: '确认清空' }));
    await waitFor(() => expect(deleteMyHistory).toHaveBeenCalledTimes(1));
  });
});
