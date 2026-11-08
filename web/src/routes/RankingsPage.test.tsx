import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { expect, test, vi } from 'vitest';
import { listRankings } from '@/api';
import { RankingsPage } from './RankingsPage';

vi.mock('@/api', () => ({ listRankings: vi.fn() }));

function entry(slug: string, rank: number, value: number, title = slug) {
  return {
    rank,
    value,
    demo: {
      slug,
      title,
      coverUrl: null,
      createdAt: '2026-11-01T00:00:00Z',
      lifecycleState: 'published',
      matchedIn: 'title',
      classifications: [],
    },
  };
}

function boardBody(board: string, range: string, items: ReturnType<typeof entry>[]) {
  return { board, range, items, total: items.length };
}

function renderAt(path: string) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[path]}>
        <RankingsPage />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

/**
 * 默认口径是平均分 + 全部时间。
 *
 * 这一条同时钉住「默认值不写进请求」：`{board:'avg', range:'all'}` 与 `{}` 是同一个榜，
 * 多传字段只让缓存键多分出一份。
 */
test('默认加载平均分榜，且不传默认参数', async () => {
  vi.mocked(listRankings).mockResolvedValue({
    status: 200,
    data: boardBody('avg', 'all', [entry('a', 1, 4.8)]),
  } as never);

  renderAt('/rankings');

  await waitFor(() => expect(listRankings).toHaveBeenCalled());
  const arg = vi.mocked(listRankings).mock.calls[0][0] as Record<string, unknown>;
  expect('board' in arg).toBe(false);
  expect('range' in arg).toBe(false);
  await waitFor(() => expect(screen.getByText('4.80 分')).toBeTruthy());
});

/**
 * 名次要显示出来，且单位的文案跟着口径走。
 *
 * 六榜的 `value` 含意不同（分数 / 票数 / 净票 / 热度分）。一律当成分显示，
 * 会在 5 分票榜上读出「12.00 分」这种不可能的平均分。
 */
test('名次与单位按口径显示', async () => {
  vi.mocked(listRankings).mockResolvedValue({
    status: 200,
    data: boardBody('god', 'all', [entry('a', 1, 12), entry('b', 2, 3)]),
  } as never);

  renderAt('/rankings?board=god');

  await waitFor(() => expect(screen.getByText('12 票')).toBeTruthy());
  expect(screen.getByText('3 票')).toBeTruthy();
  expect(screen.getByText('1')).toBeTruthy();
  expect(screen.getByText('2')).toBeTruthy();
});

/**
 * 切榜要写进 URL 并重新请求——榜单是会被分享的东西（「上周的 5 分票榜」），
 * 状态只放组件里就分享不出去。
 */
test('切榜写入 URL 并换请求参数', async () => {
  vi.mocked(listRankings).mockResolvedValue({
    status: 200,
    data: boardBody('avg', 'all', []),
  } as never);

  renderAt('/rankings');

  await waitFor(() => expect(listRankings).toHaveBeenCalled());
  fireEvent.click(screen.getByRole('button', { name: '综合热度' }));

  await waitFor(() => {
    expect(listRankings).toHaveBeenCalledWith(expect.objectContaining({ board: 'heat' }));
  });
});

/**
 * 换时间范围同理，并且要回到第 1 名（榜单没有页码，但请求里不能带旧的 offset）。
 */
test('切时间范围写入 URL 并重新请求', async () => {
  vi.mocked(listRankings).mockResolvedValue({
    status: 200,
    data: boardBody('avg', 'all', []),
  } as never);

  renderAt('/rankings');

  await waitFor(() => expect(listRankings).toHaveBeenCalled());
  fireEvent.change(screen.getByLabelText('时间范围'), { target: { value: 'week' } });

  await waitFor(() => {
    expect(listRankings).toHaveBeenCalledWith(expect.objectContaining({ range: 'week' }));
  });
});

/**
 * 空态文案分两类：评分榜是「还没有人评分」，另外两个榜是「还没有作品」。
 *
 * 四个评分榜排除零评分作品（SOC-014），所以它们空着是正常状态，
 * 不是「站里没内容」。写成同一个空态会让人以为数据没加载出来。
 */
test('评分榜空着时说「还没有人评分」', async () => {
  vi.mocked(listRankings).mockResolvedValue({
    status: 200,
    data: boardBody('avg', 'all', []),
  } as never);

  renderAt('/rankings?board=avg');

  await waitFor(() => expect(screen.getByText('还没有人评分')).toBeTruthy());
  expect(screen.queryByText('还没有作品')).toBeNull();
});

/**
 * 综合热度榜空着时说「还没有作品」。
 */
test('热度榜空着时说「还没有作品」', async () => {
  vi.mocked(listRankings).mockResolvedValue({
    status: 200,
    data: boardBody('heat', 'all', []),
  } as never);

  renderAt('/rankings?board=heat');

  await waitFor(() => expect(screen.getByText('还没有作品')).toBeTruthy());
});

/**
 * 失败可重试（SCH-012），重试是真的再发一次请求。
 */
test('加载失败时给出原因与重试入口', async () => {
  vi.mocked(listRankings).mockResolvedValue({ status: 503, data: undefined } as never);

  renderAt('/rankings');

  await waitFor(() => expect(screen.getByText('榜单加载失败')).toBeTruthy());
  expect(screen.getByText('榜单加载失败（HTTP 503）')).toBeTruthy();

  vi.mocked(listRankings).mockResolvedValue({
    status: 200,
    data: boardBody('avg', 'all', [entry('a', 1, 4)]),
  } as never);
  fireEvent.click(screen.getByRole('button', { name: '重试' }));

  await waitFor(() => expect(screen.getByText('4.00 分')).toBeTruthy());
});
