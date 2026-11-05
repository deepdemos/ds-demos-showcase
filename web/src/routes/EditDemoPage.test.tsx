import { render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router';
import { beforeEach, expect, test, vi } from 'vitest';
import { EditDemoPage } from './EditDemoPage';

/**
 * 编辑页的三条要害：
 *
 * 1. 预填的是**当前版本**的内容。空白表会让「改一个错别字」把整段简介丢掉。
 * 2. 不是作者就不能编辑——哪怕详情接口对公开作品回 200，那也是别人的作品。
 * 3. 只改文字时不传 demoType：服务端只允许「换包时顺带换类型」，
 *    前端若总是传，就会把这个约束变成「每次保存都要带包」。
 */

const getDemo = vi.fn();
const updateDemo = vi.fn();

vi.mock('@/api', () => ({
  getDemo: (...args: unknown[]) => getDemo(...args),
  updateDemo: (...args: unknown[]) => updateDemo(...args),
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
  ...over,
});

function renderPage() {
  render(
    <MemoryRouter initialEntries={['/demos/demo-a/edit']}>
      <Routes>
        <Route path="/demos/:slug/edit" element={<EditDemoPage />} />
      </Routes>
    </MemoryRouter>,
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
