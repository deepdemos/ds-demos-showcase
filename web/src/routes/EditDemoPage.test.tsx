import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router';
import { beforeEach, expect, test, vi } from 'vitest';
import { EditDemoPage } from './EditDemoPage';

/**
 * 编辑页的四条要害：
 *
 * 1. 预填的是**当前版本**的内容。空白表会让「改一个错别字」把整段简介丢掉。
 * 2. 不是作者就不能编辑——哪怕详情接口对公开作品回 200，那也是别人的作品。
 * 3. 只改文字时不传 demoType：服务端只允许「换包时顺带换类型」，
 *    前端若总是传，就会把这个约束变成「每次保存都要带包」。
 * 4. 模型声明是**整体替换**：保存时总是把当前勾选交上去，所以必须预填，
 *    否则「改个标题」会顺手把声明清空。
 */

const getDemo = vi.fn();
const updateDemo = vi.fn();

// 编辑页现在嵌了版本历史组件，它会调 listDemoRevisions。
// 不在这里补一个的话，测试全绿但 vitest 会报两条 unhandled rejection——
// 「绿了但报错」比红更危险，因为没人会去看。
vi.mock('@/api', () => ({
  getDemo: (...args: unknown[]) => getDemo(...args),
  updateDemo: (...args: unknown[]) => updateDemo(...args),
  listDemoRevisions: () => Promise.resolve({ status: 200, data: { items: [] } }),
  listModels: () =>
    Promise.resolve({
      status: 200,
      data: {
        items: [
          { slug: 'deepseek-chat', name: 'DeepSeek Chat', vendor: 'DeepSeek' },
          { slug: 'deepseek-reasoner', name: 'DeepSeek Reasoner', vendor: 'DeepSeek' },
        ],
      },
    }),
}));

// 必须返回**同一个对象**：编辑页的加载 effect 依赖 `me`，每次渲染给新对象
// 会让 effect 反复重跑 → setState → 重渲染，测试直接挂死（实测 120 秒不返回）。
const authState = { me: { username: 'alice' }, isLoading: false };
vi.mock('@/features/auth/AuthContext', () => ({
  useAuth: () => authState,
}));

const demo = (over: Record<string, unknown> = {}) => ({
  slug: 'demo-a',
  title: '原标题',
  description: '原简介',
  coverUrl: null,
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z',
  lifecycleState: 'published',
  demoType: 'web',
  downloadable: true,
  canEdit: true,
  author: { username: 'alice', displayName: 'Alice' },
  publishedAt: '2026-01-01T00:00:00Z',
  models: [{ slug: 'deepseek-chat', name: 'DeepSeek Chat', vendor: 'DeepSeek' }],
  ...over,
});

function renderPage() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={['/demos/demo-a/edit']}>
        <Routes>
          <Route path="/demos/:slug/edit" element={<EditDemoPage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  getDemo.mockReset();
  updateDemo.mockReset();
});

test('预填当前版本的标题与简介', async () => {
  getDemo.mockResolvedValue({ status: 200, data: demo() });
  renderPage();
  await waitFor(() => expect(screen.getByDisplayValue('原标题')).toBeInTheDocument());
  expect(screen.getByDisplayValue('原简介')).toBeInTheDocument();
});

test('不是作者时不出编辑表单', async () => {
  getDemo.mockResolvedValue({ status: 200, data: demo({ canEdit: false }) });
  renderPage();
  await waitFor(() => expect(screen.getByText('不能编辑这条作品')).toBeInTheDocument());
  expect(screen.queryByDisplayValue('原标题')).toBeNull();
});

test('只改文字时不传 demoType', async () => {
  getDemo.mockResolvedValue({ status: 200, data: demo() });
  updateDemo.mockResolvedValue({ status: 200, data: { slug: 'demo-a', revisionNo: 2 } });
  renderPage();
  await waitFor(() => expect(screen.getByDisplayValue('原标题')).toBeInTheDocument());

  screen.getByRole('button', { name: '保存并提交复审' }).click();

  await waitFor(() => expect(updateDemo).toHaveBeenCalled());
  const [, body] = updateDemo.mock.calls[0] as [string, Record<string, unknown>];
  expect(body.demoType).toBeUndefined();
  expect(body.package).toBeUndefined();

  await waitFor(() => expect(screen.getByText(/第 2 版已提交/)).toBeInTheDocument());
});

/**
 * 预填声明，且保存时原样交回去。
 *
 * 「整体替换」语义下，不预填 = 每次保存都清空。这条用例同时钉住两件事：
 * 打开时勾着原来那个、保存时它还在请求体里。
 */
test('预填已有的模型声明，保存时原样交回', async () => {
  getDemo.mockResolvedValue({ status: 200, data: demo() });
  updateDemo.mockResolvedValue({ status: 200, data: { slug: 'demo-a', revisionNo: 2 } });
  renderPage();

  const box = await screen.findByLabelText('DeepSeek Chat（DeepSeek）');
  expect(box).toBeChecked();

  screen.getByRole('button', { name: '保存并提交复审' }).click();

  await waitFor(() => expect(updateDemo).toHaveBeenCalled());
  const [, body] = updateDemo.mock.calls[0] as [string, Record<string, unknown>];
  expect(body.modelIds).toEqual(['deepseek-chat']);
});

/**
 * 取消勾选就是清空。
 *
 * 这条是「整体替换」的另一半：服务端把「没传」与「传空」都当清空，
 * 前端则必须**显式**传一个数组，让「我确实不要这些声明了」成为一个可表达的意图。
 */
test('取消勾选后保存，声明被清空', async () => {
  getDemo.mockResolvedValue({ status: 200, data: demo() });
  updateDemo.mockResolvedValue({ status: 200, data: { slug: 'demo-a', revisionNo: 2 } });
  renderPage();

  const box = await screen.findByLabelText('DeepSeek Chat（DeepSeek）');
  fireEvent.click(box);
  expect(box).not.toBeChecked();

  screen.getByRole('button', { name: '保存并提交复审' }).click();

  await waitFor(() => expect(updateDemo).toHaveBeenCalled());
  const [, body] = updateDemo.mock.calls[0] as [string, Record<string, unknown>];
  expect(body.modelIds).toEqual([]);
});
