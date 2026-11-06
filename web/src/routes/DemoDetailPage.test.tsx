import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router';
import { expect, test, vi } from 'vitest';
import { getDemo } from '@/api';
import { DemoDetailPage, DemoDownload, DemoPreview, tombstoneText } from './DemoDetailPage';

vi.mock('@/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/api')>();
  return {
    ...actual,
    getDemo: vi.fn(),
    // 记浏览是 fire-and-forget：不 mock 就会去打真 fetch，落进 catch 里静默失败，
    // 测试仍是绿的但控制台会有一串噪音。
    postDemoView: () => Promise.resolve({ status: 204 }),
  };
});

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
  moderationState: 'approved',
  viewCount: 0,
  downloadCount: 0,
  lifecycleActions: { edit: false, archive: false, restore: false, delete: false },
  author: { username: 'alice', displayName: 'Alice' },
  models: [],
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
    }),
  } as never);

  renderPage();

  await waitFor(() => expect(screen.getByText('作者 Alice')).toBeInTheDocument());
  // createdAt 是 1 月、publishedAt 是 2 月：页面必须显示后者。
  expect(screen.getByText(/发布 2026\/2\/3/)).toBeInTheDocument();
  expect(screen.getByText(/模型声明：DeepSeek Chat/)).toBeInTheDocument();
  expect(screen.getByText(/作者自述，未经核验/)).toBeInTheDocument();
});

/**
 * 没有声明时不出现「模型声明：」这一行。
 *
 * 空数组还渲染的话，页面会出现一行「模型声明：」后面什么都没有——
 * 读者会以为加载失败。
 */
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
