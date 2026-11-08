import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, useNavigate } from 'react-router';
import { expect, test, vi } from 'vitest';
import { listDemos, listRecommendations } from '@/api';
import { DemosPage } from './DemosPage';

vi.mock('@/api', () => ({ listDemos: vi.fn(), listRecommendations: vi.fn() }));

function emptyPage() {
  return { items: [], page: 1, pageSize: 12, total: 0 };
}

function renderAt(initialPath: string, recommendation: unknown = { status: 401 }) {
  // 默认按访客处理：推荐条整块不渲染，绝大多数用例关心的是列表本身。
  // 需要推荐的用例把响应体当第二个参数传进来——不能在 renderAt 之后再 mock，
  // 那样默认值已经把之前的设置覆盖掉了。
  vi.mocked(listRecommendations).mockResolvedValue(recommendation as never);
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[initialPath]}>
        <DemosPage />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

/**
 * 搜索词进 URL 查询参数。
 *
 * 这一条钉的是「搜到的页面能分享、刷新不丢」——如果把搜索词放在组件 state 里，
 * 用户复制地址栏发给别人，对方看到的是全部作品。
 */
test('把搜索词带进请求', async () => {
  vi.mocked(listDemos).mockResolvedValue({ status: 200, data: emptyPage() } as never);

  renderAt('/demos?q=探针');

  await waitFor(() => {
    expect(listDemos).toHaveBeenCalledWith(expect.objectContaining({ q: '探针' }));
  });
  expect(screen.getByLabelText('搜索作品')).toHaveProperty('value', '探针');
});

/**
 * 没有搜索词时**不传** q 字段。
 *
 * 传 `q: ''` 也能工作，但会让缓存键多分出一份（`['demos',1,'']` 与 `['demos',1]`），
 * 而且清空搜索后看起来像没生效——所以请求里干脆别有这个字段。
 */
test('没有搜索词时不传 q', async () => {
  vi.mocked(listDemos).mockResolvedValue({ status: 200, data: emptyPage() } as never);

  renderAt('/demos');

  await waitFor(() => expect(listDemos).toHaveBeenCalled());
  const arg = vi.mocked(listDemos).mock.calls[0][0] as Record<string, unknown>;
  expect('q' in arg).toBe(false);
});

/**
 * 搜不到时的文案要和「还没有作品」分开。
 *
 * 同一个空状态用在两种场景，用户会以为是站里没东西，而不是自己的词没命中。
 */
test('搜不到时给出与「还没有作品」不同的空状态', async () => {
  vi.mocked(listDemos).mockResolvedValue({ status: 200, data: emptyPage() } as never);

  renderAt('/demos?q=不存在的词');

  await waitFor(() => {
    expect(screen.getByText('没有匹配的作品')).toBeTruthy();
  });
  expect(screen.queryByText('还没有公开的作品')).toBeNull();
});

/**
 * 搜不到时，空态自己带一个「清除搜索」。
 *
 * 事故现场：空态只说「换个词，或者清除搜索看全部」，而「清除」按钮在工具栏里，
 * 用户在页面下方找不到它；空态本身连一个可点的动作都没有。
 */
test('搜不到时，空态里能直接清除搜索', async () => {
  vi.mocked(listDemos).mockResolvedValue({ status: 200, data: emptyPage() } as never);

  renderAt('/demos?q=不存在的词&page=3');

  fireEvent.click(await screen.findByRole('button', { name: '清除搜索' }));

  await waitFor(() => {
    expect(screen.getByText('还没有公开的作品')).toBeTruthy();
  });
  // 清的是 q 和 page 两个参数：只清 q 会停在第 3 页，用户看起来像「点了没反应」。
  const last = vi.mocked(listDemos).mock.calls.at(-1)?.[0] as Record<string, unknown>;
  expect('q' in last).toBe(false);
  expect(last.page).toBe(1);
});

/**
 * 简介命中要标出来。
 *
 * 只有简介命中时，卡片标题里看不到搜索词——不标注，用户会以为结果不相关。
 */
test('简介命中的结果带标注', async () => {
  vi.mocked(listDemos).mockResolvedValue({
    status: 200,
    data: {
      items: [
        {
          slug: 'a',
          title: '标题里没有关键词',
          coverUrl: null,
          createdAt: '2026-11-01T00:00:00Z',
          lifecycleState: 'published',
          matchedIn: 'description',
        },
      ],
      page: 1,
      pageSize: 12,
      total: 1,
    },
  } as never);

  renderAt('/demos?q=零拷贝');

  await waitFor(() => {
    expect(screen.getByText('简介命中')).toBeTruthy();
  });
});

/**
 * 输入框要跟着 URL 走。
 *
 * 从别的页面点链接进来、或按浏览器后退时，URL 变了而输入框还停在旧词上，
 * 看起来就像「搜索没生效」。这里用真的 navigate 触发，不是重渲染
 * （`MemoryRouter` 的 `initialEntries` 只在挂载时读一次）。
 */
test('URL 里的关键词变化时输入框跟着变', async () => {
  vi.mocked(listDemos).mockResolvedValue({ status: 200, data: emptyPage() } as never);
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });

  function Nav() {
    const navigate = useNavigate();
    return (
      <button type="button" onClick={() => void navigate('/demos?q=回滚')}>
        去回滚
      </button>
    );
  }

  render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={['/demos?q=探针']}>
        <Nav />
        <DemosPage />
      </MemoryRouter>
    </QueryClientProvider>,
  );
  await waitFor(() => expect(screen.getByLabelText('搜索作品')).toHaveProperty('value', '探针'));

  fireEvent.click(screen.getByRole('button', { name: '去回滚' }));

  await waitFor(() => expect(screen.getByLabelText('搜索作品')).toHaveProperty('value', '回滚'));
});

/**
 * 提交搜索表单会发起请求。
 *
 * 这条只验「输入 + 提交」这条链路是通的；URL 更新的细节交给上面的用例。
 */
test('输入关键词并提交后发起请求', async () => {
  vi.mocked(listDemos).mockResolvedValue({ status: 200, data: emptyPage() } as never);

  renderAt('/demos');
  await waitFor(() => expect(listDemos).toHaveBeenCalled());

  fireEvent.change(screen.getByLabelText('搜索作品'), { target: { value: '探针' } });
  fireEvent.click(screen.getByRole('button', { name: '搜索' }));

  await waitFor(() => {
    expect(listDemos).toHaveBeenCalledWith(expect.objectContaining({ q: '探针' }));
  });
});

/**
 * 换搜索词要回到第 1 页。
 *
 * 停在原来的页码上，结果只会更少：第 3 页搜一个只有 2 条命中的词就是空白，
 * 用户看到的「搜不到」其实是「你在第 3 页」。
 */
test('换搜索词时回到第 1 页', async () => {
  vi.mocked(listDemos).mockResolvedValue({ status: 200, data: emptyPage() } as never);

  renderAt('/demos?page=3&q=旧词');
  await waitFor(() => expect(listDemos).toHaveBeenCalled());

  fireEvent.change(screen.getByLabelText('搜索作品'), { target: { value: '新词' } });
  fireEvent.click(screen.getByRole('button', { name: '搜索' }));

  await waitFor(() => {
    expect(listDemos).toHaveBeenCalledWith(expect.objectContaining({ q: '新词', page: 1 }));
  });
});

/**
 * 排序进 URL，切到热度会回到第 1 页（SCH-010）。
 *
 * 停在第 3 页再换排序，看到的是「新一轮的第 3 页」——空白或错位都可能，
 * 看起来像换排序坏了。所以换排序必须与换搜索词、换筛选同等对待。
 */
test('切到热度排序时带上 sort 并回到第 1 页', async () => {
  vi.mocked(listDemos).mockResolvedValue({ status: 200, data: emptyPage() } as never);

  renderAt('/demos?page=3');

  await waitFor(() => expect(listDemos).toHaveBeenCalled());
  fireEvent.change(screen.getByLabelText('排序'), { target: { value: 'hot' } });

  await waitFor(() => {
    expect(listDemos).toHaveBeenCalledWith(expect.objectContaining({ sort: 'hot' }));
  });
  const arg = vi.mocked(listDemos).mock.calls.at(-1)?.[0] as Record<string, unknown>;
  expect(arg.page).toBe(1);
});

/**
 * 随机发现的 seed 必须来自 URL，并且回写到请求里。
 *
 * seed 是「这一轮抽样」的标识：翻页、刷新、后退都要落在同一轮上，
 * 否则下一页会与上一页重叠或漏项（SCH-007）。所以它不能只存在组件 state 里——
 * 那样一刷新就换了一轮，第 2 页拿到的就是另一批。
 */
test('随机发现的 seed 从 URL 来，跟着请求走', async () => {
  vi.mocked(listDemos).mockResolvedValue({ status: 200, data: emptyPage() } as never);

  renderAt('/demos?sort=random&seed=12345&page=2');

  await waitFor(() => {
    expect(listDemos).toHaveBeenCalledWith(
      expect.objectContaining({ sort: 'random', seed: 12345, page: 2 }),
    );
  });
});

/**
 * 切到随机发现时要换一个新种子，并回到第 1 页。
 *
 * 「随机发现」的语义是换一批，不是「同一批换个顺序」——切过去若沿用上一个种子，
 * 用户点两次会看到同样的结果，会以为随机坏了。
 */
test('切到随机发现会写一个新 seed 并回到第 1 页', async () => {
  vi.mocked(listDemos).mockResolvedValue({ status: 200, data: emptyPage() } as never);

  renderAt('/demos?page=3&sort=latest');

  await waitFor(() => expect(listDemos).toHaveBeenCalled());
  fireEvent.change(screen.getByLabelText('排序'), { target: { value: 'random' } });

  await waitFor(() => {
    expect(listDemos).toHaveBeenCalledWith(expect.objectContaining({ sort: 'random' }));
  });
  const arg = vi.mocked(listDemos).mock.calls.at(-1)?.[0] as Record<string, unknown>;
  expect(arg.page).toBe(1);
  expect(typeof arg.seed).toBe('number');
  expect(arg.seed as number).toBeGreaterThanOrEqual(0);
});

test('登录后有推荐时在列表上方显示', async () => {
  vi.mocked(listDemos).mockResolvedValue({ status: 200, data: emptyPage() } as never);

  renderAt('/demos', {
    status: 200,
    data: {
      items: [
        {
          slug: 'demo-rec',
          title: '你可能想看这件',
          matchedIn: 'title',
          createdAt: '2026-01-05T00:00:00.000Z',
          lifecycleState: 'published',
          classifications: [{ dimension: 'genre', slug: 'game', label: '小游戏' }],
        },
      ],
    },
  });

  expect(await screen.findByText('为你推荐')).toBeTruthy();
  expect(screen.getByText('你可能想看这件')).toBeTruthy();
  expect(screen.getByText('小游戏')).toBeTruthy();
  expect(screen.getByRole('link', { name: /你可能想看这件/ }).getAttribute('href')).toBe(
    '/demos/demo-rec',
  );
});

test('访客不显示推荐条', async () => {
  vi.mocked(listDemos).mockResolvedValue({ status: 200, data: emptyPage() } as never);

  renderAt('/demos');

  // 等列表本身落定，再断言推荐条不在——否则可能在推荐请求还没回来时就断言了。
  await screen.findByText('还没有公开的作品');
  expect(screen.queryByText('为你推荐')).toBeNull();
});
