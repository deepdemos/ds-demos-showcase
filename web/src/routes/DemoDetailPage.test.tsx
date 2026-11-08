import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router';
import { expect, test, vi } from 'vitest';
import {
  deleteDemoRating,
  getDemo,
  listDemoTimeline,
  listRelatedDemos,
  putDemoRating,
} from '@/api';
import { DemoDetailPage, DemoDownload, DemoPreview, tombstoneText } from './DemoDetailPage';

vi.mock('@/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/api')>();
  return {
    ...actual,
    getDemo: vi.fn(),
    // 记浏览是 fire-and-forget：不 mock 就会去打真 fetch，落进 catch 里静默失败，
    // 测试仍是绿的但控制台会有一串噪音。
    postDemoView: () => Promise.resolve({ status: 204 }),
    putDemoRating: vi.fn(),
    deleteDemoRating: vi.fn(),
    // 作品动态：作者视图会挂这个区块，不 mock 就会去发真请求。
    listDemoTimeline: vi.fn(),
    // 相关作品同理：详情页尾部总会挂它。
    listRelatedDemos: vi.fn(),
  };
});

// 默认「没有任何动作」：绝大多数用例关心的是详情本身，不是动态。
// 需要动作的用例自己 mockResolvedValue 覆盖。
const timelineEmpty = { status: 200, data: { items: [] } };

const detail = (over: Record<string, unknown> = {}) => ({
  slug: 'demo-a',
  title: '钢琴卷帘',
  description: '把 MIDI 画成瀑布的样子。',
  coverUrl: null,
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z',
  publishedAt: '2026-02-03T00:00:00Z',
  lifecycleState: 'published',
  demoType: 'web',
  downloadable: false,
  canEdit: false,
  isAuthor: false,
  moderationState: 'approved',
  viewCount: 0,
  downloadCount: 0,
  rating: { average: 0, count: 0, mine: null },
  lifecycleActions: { edit: false, archive: false, restore: false, delete: false },
  author: { username: 'alice', displayName: 'Alice' },
  models: [],
  classifications: [],
  ...over,
});

function renderPage() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={['/demos/demo-a']}>
        <Routes>
          <Route path="/demos/:slug" element={<DemoDetailPage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

/**
 * 墓碑文案按「怎么消失的」分三种。标准对象 §ST-2 要求 archived 与 removed 的墓碑内容
 * **必须不同**——一个是作者自己下架，一个是平台移除，混成一句「作品不存在」会让作者
 * 以为自己的东西被删了。
 */
test('归档：文案说的是作者自己下架', () => {
  const text = tombstoneText({ lifecycleState: 'archived' });
  expect(text.title).toContain('作者已下架');
});

test('移除：文案说的是平台移除，不能与归档同句', () => {
  const archived = tombstoneText({ lifecycleState: 'archived' });
  const removed = tombstoneText({ lifecycleState: 'removed' });
  expect(removed.title).toContain('已被移除');
  expect(removed.title).not.toBe(archived.title);
  expect(removed.description).not.toBe(archived.description);
});

/**
 * 软删不再是墓碑。
 *
 * 旧断言写「已删除但 lifecycle 仍是 published 时走三种文案之一」，那是把
 * `deleted_at` 当成了墓碑的一种。生命周期 §4.1 相反：删了就是不存在（对访客 404），
 * 墓碑只属于 `archived` / `removed`。所以这里改为钉「非墓碑状态给兜底文案」，
 * 并确保兜底文案**不与 archived/removed 混同**——真走到它说明前后端假设不一致。
 */
test('非墓碑状态走兜底文案，且不与下架/移除混同', () => {
  const fallback = tombstoneText({ lifecycleState: 'published' });
  const archived = tombstoneText({ lifecycleState: 'archived' });
  const removed = tombstoneText({ lifecycleState: 'removed' });
  expect(fallback.title).not.toBe(archived.title);
  expect(fallback.title).not.toBe(removed.title);
});

/**
 * 预览的 sandbox 属性是第二道隔离（第一道是服务端 CSP）。
 * 这两条断言不是形式检查：`allow-same-origin` 一加，脚本就能读主站的
 * cookie 与 localStorage，然后带着访问者身份去调任何接口。
 */
test('预览 iframe 不带 allow-same-origin', () => {
  render(
    <MemoryRouter>
      <DemoPreview slug="demo-abc" />
    </MemoryRouter>,
  );
  const frame = screen.getByTitle('作品预览');
  const sandbox = frame.getAttribute('sandbox') ?? '';
  expect(sandbox).toContain('allow-scripts');
  expect(sandbox).not.toContain('allow-same-origin');
  expect(sandbox).not.toContain('allow-top-navigation');
});

test('预览 iframe 指向该作品的预览路径', () => {
  render(
    <MemoryRouter>
      <DemoPreview slug="demo-abc" />
    </MemoryRouter>,
  );
  expect(screen.getByTitle('作品预览')).toHaveAttribute('src', '/preview/demo-abc/');
});

/**
 * 下载入口的 href 必须是 /download/{slug}。
 *
 * 这条断言钉的是「与 `/preview` 分家」这个决定：把下载塞回预览路径，
 * 浏览器会按 Content-Type 渲染而不是落盘，作品里的脚本就跑到页面里来了。
 */
test('下载入口指向该作品的下载路径', () => {
  render(
    <MemoryRouter>
      <DemoDownload slug="demo-abc" />
    </MemoryRouter>,
  );
  expect(screen.getByRole('link', { name: '下载作品' })).toHaveAttribute(
    'href',
    '/download/demo-abc',
  );
});

/**
 * 作者、发布时间、模型声明这三项是 DEMO-042 九项里最容易被漏掉的三项——
 * 它们在库里一直有值，只是没人读出来过。
 *
 * 发布时间断言的是「渠道发布时间」而不是 `createdAt`：两个日期故意给成不同的月份，
 * 取错了断言会红。
 */
test('详情显示作者、发布时间与模型声明', async () => {
  vi.mocked(getDemo).mockResolvedValue({
    status: 200,
    data: detail({
      models: [{ slug: 'deepseek-chat', name: 'DeepSeek Chat', vendor: 'DeepSeek' }],
      classifications: [
        { dimension: 'genre', slug: 'game', label: '小游戏' },
        { dimension: 'technique', slug: '3D建模', label: '3D建模' },
      ],
    }),
  } as never);

  renderPage();

  await waitFor(() => expect(screen.getByText('作者 Alice')).toBeInTheDocument());
  // createdAt 是 1 月、publishedAt 是 2 月：页面必须显示后者。
  expect(screen.getByText(/发布 2026\/2\/3/)).toBeInTheDocument();
  // 模型名现在是链接（点进模型页），所以「模型声明：」与模型名不在同一个文本节点里。
  expect(screen.getByText(/模型声明：/)).toBeInTheDocument();
  const modelLink = screen.getByRole('link', { name: 'DeepSeek Chat' });
  expect(modelLink).toHaveAttribute('href', '/models/deepseek-chat');
  expect(screen.getByText(/作者自述，未经核验/)).toBeInTheDocument();
});

/**
 * 没有声明时不出现「模型声明：」这一行。
 *
 * 空数组还渲染的话，页面会出现一行「模型声明：」后面什么都没有——
 * 读者会以为加载失败。
 */
/**
 * 分类要显示出来。
 *
 * 分类挂作品、模型挂版本，两处渲染的是不同的数据。漏掉分类的表现是
 * 「作者打了标签但页面上什么都没有」，而且不报任何错。
 *
 * 链接也一起断言：分类标签是入口，指错了地方（比如少 encode 中文）
 * 会把用户带到 404，而页面本身看起来完全正常。
 */
test('详情显示作品分类，并链到对应筛选', async () => {
  vi.mocked(getDemo).mockResolvedValue({
    status: 200,
    data: detail({
      classifications: [
        { dimension: 'genre', slug: 'game', label: '小游戏' },
        { dimension: 'technique', slug: '3D建模', label: '3D建模' },
      ],
    }),
  } as never);
  renderPage();

  expect(await screen.findByText('分类')).toBeInTheDocument();
  expect(screen.getByRole('link', { name: '小游戏' })).toHaveAttribute('href', '/demos?genre=game');
  expect(screen.getByRole('link', { name: '3D建模' })).toHaveAttribute(
    'href',
    `/demos?technique=${encodeURIComponent('3D建模')}`,
  );
});

test('没有模型声明时不渲染那一行', async () => {
  vi.mocked(getDemo).mockResolvedValue({ status: 200, data: detail() } as never);
  renderPage();
  await waitFor(() => expect(screen.getByText('作者 Alice')).toBeInTheDocument());
  expect(screen.queryByText(/模型声明：/)).toBeNull();
});

/**
 * 作者看自己还没过审的作品时，页面必须说出「审核中」。
 *
 * 不说的话，作者看到的是一个正常的详情页（标题、简介、统计全在），
 * 会以为作品已经上线，然后到作品库里找不到自己。
 */
test('作者看待审作品时看到审核中', async () => {
  vi.mocked(getDemo).mockResolvedValue({
    status: 200,
    data: detail({ canEdit: true, moderationState: 'pending', lifecycleState: 'draft' }),
  } as never);

  renderPage();

  await waitFor(() => expect(screen.getByText(/审核中/)).toBeInTheDocument());
});

/**
 * 「已发布 + 待审」是作者改稿的正常状态：对外内容由 published_revision_id 决定，
 * 审核态只描述当前版本。那时作品还在库里、也还发布着——文案必须说这件事，
 * 否则作者会以为自己的作品下线了，下一步就是再去点一次发布。
 */
test('已发布的作品改稿待审时，文案说的是「线上仍是上一版」', async () => {
  vi.mocked(getDemo).mockResolvedValue({
    status: 200,
    data: detail({ canEdit: true, moderationState: 'pending', lifecycleState: 'published' }),
  } as never);

  renderPage();

  await waitFor(() => expect(screen.getByText(/新版本审核中/)).toBeInTheDocument());
  expect(screen.getByText(/线上仍是上一版/)).toBeInTheDocument();
  expect(screen.queryByText(/不会出现在作品库里/)).toBeNull();
});

test('已发布的作品改稿被驳回时，也先说线上仍在服务', async () => {
  vi.mocked(getDemo).mockResolvedValue({
    status: 200,
    data: detail({ canEdit: true, moderationState: 'rejected', lifecycleState: 'published' }),
  } as never);

  renderPage();

  await waitFor(() => expect(screen.getByText(/新版本未通过审核/)).toBeInTheDocument());
  expect(screen.getByText(/线上仍是上一版/)).toBeInTheDocument();
});

/**
 * 已通过的作品不显示这条——否则每个作者打开自己的作品都会看到一句「审核中」。
 */
test('已通过的作品不显示审核提示', async () => {
  vi.mocked(getDemo).mockResolvedValue({
    status: 200,
    data: detail({ canEdit: true, moderationState: 'approved' }),
  } as never);

  renderPage();

  await waitFor(() => expect(screen.getByText('作者 Alice')).toBeInTheDocument());
  expect(screen.queryByText(/审核中/)).toBeNull();
  expect(screen.queryByText(/审核未通过/)).toBeNull();
});

/**
 * 评分区的三件事。缺任何一条这块就是摆设：显示不出来 = 没人知道有评分；
 * 点星不写 = 打了等于没打；错误不显示 = 用户以为打上了。
 */
test('详情显示平均分与票数', async () => {
  vi.mocked(getDemo).mockResolvedValue({
    status: 200,
    data: detail({ rating: { average: 4.5, count: 12, mine: null } }),
  } as never);
  renderPage();
  await waitFor(() => expect(screen.getByText(/平均 4\.50 分，12 人评过/)).toBeTruthy());
});

test('点星会把分数写回去', async () => {
  vi.mocked(putDemoRating).mockResolvedValue({
    status: 200,
    data: { average: 5, count: 1, mine: 5 },
  } as never);
  vi.mocked(getDemo).mockResolvedValue({
    status: 200,
    data: detail({ rating: { average: 0, count: 0, mine: null } }),
  } as never);
  renderPage();
  await waitFor(() => expect(screen.getByText(/还没有人评分/)).toBeTruthy());
  screen.getByRole('radio', { name: '打 5 分' }).click();
  await waitFor(() =>
    expect(vi.mocked(putDemoRating)).toHaveBeenCalledWith('demo-a', { score: 5 }),
  );
});

test('点已选中的那颗星是撤销', async () => {
  vi.mocked(deleteDemoRating).mockResolvedValue({
    status: 200,
    data: { average: 0, count: 0, mine: null },
  } as never);
  vi.mocked(getDemo).mockResolvedValue({
    status: 200,
    data: detail({ rating: { average: 3, count: 1, mine: 3 } }),
  } as never);
  renderPage();
  const selected = await screen.findByRole('radio', { name: /取消评分/ });
  selected.click();
  await waitFor(() => expect(vi.mocked(deleteDemoRating)).toHaveBeenCalledWith('demo-a'));
});

test('打分失败时把原因显示出来', async () => {
  vi.mocked(putDemoRating).mockResolvedValue({ status: 503 } as never);
  vi.mocked(getDemo).mockResolvedValue({
    status: 200,
    data: detail({ rating: { average: 0, count: 0, mine: null } }),
  } as never);
  renderPage();
  // 等数据到位再点：详情是异步查询，抢在渲染前点会点到空气（这也是
  // 「点了没反应」在真实页面里最容易被误判成实现坏了的场景）。
  await waitFor(() => expect(screen.getByText(/还没有人评分/)).toBeTruthy());
  screen.getByRole('radio', { name: '打 2 分' }).click();
  await waitFor(() => expect(screen.getByRole('alert').textContent).toContain('打分失败'));
});

// 作品动态：服务端只给作者，所以这里要钉两件事——作者看得到，访客连请求都不发。
test('作者能看到作品动态，按时间倒序显示动作与操作人', async () => {
  vi.mocked(listDemoTimeline).mockResolvedValue({
    status: 200,
    data: {
      items: [
        {
          kind: 'publish',
          at: '2026-02-03T10:00:00Z',
          message: '发布到 deep',
          actorName: null,
        },
        {
          kind: 'approve',
          at: '2026-02-03T09:58:00Z',
          message: '内容合格',
          actorName: 'Alice',
        },
      ],
    },
  } as never);
  vi.mocked(getDemo).mockResolvedValue({
    status: 200,
    data: detail({ isAuthor: true, canEdit: true }),
  } as never);

  renderPage();

  await waitFor(() => expect(screen.getByText('作品动态')).toBeTruthy());
  // 等动态本身渲染出来：标题在 isPending 时就已经在了，
  // 只等标题会抢在列表渲染之前断言（第一版就红在这里）。
  await waitFor(() => expect(screen.getByText('发布到 deep')).toBeTruthy());
  // 已知动作给中文，未知动作显示原始 kind（不要显示「未知动作」）。
  expect(screen.getByText('发布', { selector: 'span.font-medium' })).toBeTruthy();
  expect(screen.getByText('审核通过')).toBeTruthy();
  expect(screen.getByText('· Alice')).toBeTruthy();
  expect(vi.mocked(listDemoTimeline)).toHaveBeenCalledWith('demo-a');
});

test('访客不发作品动态请求', async () => {
  vi.mocked(getDemo).mockResolvedValue({
    status: 200,
    data: detail({ isAuthor: false }),
  } as never);

  renderPage();

  await waitFor(() => expect(screen.getByText('钢琴卷帘')).toBeTruthy());
  expect(vi.mocked(listDemoTimeline)).not.toHaveBeenCalled();
  expect(screen.queryByText('作品动态')).toBeNull();
});

test('还没有动作时说明「这里会记录什么」而不是空白', async () => {
  vi.mocked(listDemoTimeline).mockResolvedValue(timelineEmpty as never);
  vi.mocked(getDemo).mockResolvedValue({
    status: 200,
    data: detail({ isAuthor: true, canEdit: true }),
  } as never);

  renderPage();

  await waitFor(() => expect(screen.getByText('还没有动作')).toBeTruthy());
});

test('相关作品与分类标签一起显示', async () => {
  vi.mocked(getDemo).mockResolvedValue({ status: 200, data: detail() } as never);
  vi.mocked(listRelatedDemos).mockResolvedValue({
    status: 200,
    data: {
      items: [
        {
          slug: 'demo-b',
          title: '同分类的邻居',
          matchedIn: 'title',
          createdAt: '2026-01-05T00:00:00.000Z',
          lifecycleState: 'published',
          classifications: [{ dimension: 'genre', slug: 'game', label: '小游戏' }],
        },
      ],
    },
  } as never);

  renderPage();

  // 列表是查出来的，标题要等一次渲染。
  expect(await screen.findByText('相关作品')).toBeTruthy();
  expect(screen.getByText('同分类的邻居')).toBeTruthy();
  expect(screen.getByText('小游戏')).toBeTruthy();
  expect(screen.getByRole('link', { name: /同分类的邻居/ }).getAttribute('href')).toBe(
    '/demos/demo-b',
  );
});

test('没有相关作品时不渲染那个区块', async () => {
  vi.mocked(getDemo).mockResolvedValue({ status: 200, data: detail() } as never);
  vi.mocked(listRelatedDemos).mockResolvedValue({ status: 200, data: { items: [] } } as never);

  renderPage();

  await waitFor(() => expect(vi.mocked(listRelatedDemos)).toHaveBeenCalled());
  // 空区块比「暂无相关作品」更便宜：读者不该为一个空框付出注意力。
  expect(screen.queryByText('相关作品')).toBeNull();
});
