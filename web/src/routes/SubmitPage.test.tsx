import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router';
import { beforeEach, describe, expect, test, vi } from 'vitest';
import { submitDemo } from '@/api';
import { SubmitPage } from './SubmitPage';

const auth = vi.hoisted(() => ({
  me: null as null | { sub: string; username: string; displayName: string },
  isLoading: false,
}));

vi.mock('@/features/auth/AuthContext', () => ({
  useAuth: () => ({ ...auth, isError: false, setMe: vi.fn(), signOut: vi.fn() }),
}));

const models = vi.hoisted(() => ({
  items: [
    { slug: 'deepseek-chat', name: 'DeepSeek Chat', vendor: 'DeepSeek' },
    { slug: 'deepseek-reasoner', name: 'DeepSeek Reasoner', vendor: 'DeepSeek' },
  ],
}));

// 投稿页现在会读模型目录（`GET /api/v1/models`）。不 mock 的话 jsdom 会去打真 fetch，
// 落进「目录读不到」那条分支——测试仍然是绿的，但测的已经不是正常路径了。
const classificationCatalog = {
  dimensions: [
    { dimension: 'genre', label: '形态', mode: 'fixed' },
    { dimension: 'technique', label: '技术', mode: 'fixed' },
    { dimension: 'subject', label: '题材', mode: 'open' },
  ],
  items: [
    { dimension: 'genre', slug: 'game', label: '小游戏', demoCount: 3 },
    { dimension: 'technique', slug: '3D建模', label: '3D建模', demoCount: 1 },
  ],
};

// 投稿页还会读分类目录（`GET /api/v1/classifications`）。不 mock 同样会打到真 fetch，
// 落进「目录读不到」的分支——组件会把分类区渲染成一句提示，测试看着绿，但没测到选择器。
// 类型与它收的形态是主数据，投稿页从 `/demo-types` 读。这里给全六种，
// 让「可选的按清单渲染、暂未开放的只做提示」这两条都被覆盖到。
const demoTypes = vi.hoisted(() => ({
  items: [
    { code: 'web', name: '网页作品', formats: ['single:html', 'single:svg', 'zip:html'] },
    { code: 'zip', name: '压缩包作品', formats: ['zip:html'] },
    { code: 'godot', name: 'Godot 游戏', formats: ['zip:godot'] },
    { code: 'image', name: '图片', formats: ['single:png', 'single:jpg', 'single:webp'] },
    { code: 'model', name: '三维模型', formats: ['single:glb'] },
    { code: 'link', name: '外链', formats: [] },
  ],
}));

const listDemoTypesMock = vi.hoisted(() => vi.fn());

vi.mock('@/api', () => ({
  submitDemo: vi.fn(),
  listDemoTypes: listDemoTypesMock,
  listModels: () => Promise.resolve({ status: 200, data: models }),
  listClassifications: () => Promise.resolve({ status: 200, data: classificationCatalog }),
}));

function renderAt(path: string) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route path="/submit" element={<SubmitPage />} />
          <Route path="/login" element={<div>登录页</div>} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

function pickFile() {
  const input = document.querySelector('input[type="file"]') as HTMLInputElement;
  const file = new File(['<html></html>'], 'index.html', { type: 'text/html' });
  fireEvent.change(input, { target: { files: [file] } });
}

beforeEach(() => {
  vi.mocked(submitDemo).mockReset();
  listDemoTypesMock.mockReset();
  listDemoTypesMock.mockResolvedValue({ status: 200, data: demoTypes });
});

describe('SubmitPage', () => {
  test('未登录时跳登录页并带上 return_to', () => {
    auth.me = null;
    auth.isLoading = false;
    renderAt('/submit');
    expect(screen.getByText('登录页')).toBeInTheDocument();
  });

  test('没有选文件时提交给出提示，不发请求', () => {
    auth.me = { sub: 'x', username: 'u', displayName: 'U' };
    auth.isLoading = false;
    renderAt('/submit');

    fireEvent.change(screen.getByLabelText('标题'), { target: { value: '我的作品' } });
    fireEvent.click(screen.getByRole('button', { name: '提交投稿' }));

    expect(screen.getByText('请选择要上传的作品文件')).toBeInTheDocument();
    expect(submitDemo).not.toHaveBeenCalled();
  });

  /**
   * 声明必须真的进请求体。
   *
   * 这条钉的是「勾了却没提交」这一整类：模型声明是可选字段，漏传不会报错、
   * 不会有日志，只会让作者以为自己声明过了。所以断言落在请求体上。
   */
  test('勾选的模型跟着投稿一起提交', async () => {
    auth.me = { sub: 'x', username: 'u', displayName: 'U' };
    auth.isLoading = false;
    vi.mocked(submitDemo).mockResolvedValue({
      status: 201,
      data: { slug: 'demo-a', lifecycleState: 'draft', moderationState: 'pending' },
    } as never);

    renderAt('/submit');

    fireEvent.change(screen.getByLabelText('标题'), { target: { value: '我的作品' } });
    pickFile();

    // 目录是异步读的，等它渲染出来再勾。
    const box = await screen.findByLabelText('DeepSeek Reasoner（DeepSeek）');
    fireEvent.click(box);

    fireEvent.click(screen.getByRole('button', { name: '提交投稿' }));

    await waitFor(() => expect(submitDemo).toHaveBeenCalled());
    const [body] = vi.mocked(submitDemo).mock.calls[0] as unknown as [Record<string, unknown>];
    expect(body.modelSlugs).toEqual(['deepseek-reasoner']);
  });

  /**
   * 勾选的分类必须真的进请求体。
   *
   * 分类是可选字段，漏传不会报错、不会有日志——只会让作者以为作品被归了类，
   * 而库里什么都没有。所以断言落在请求体上，不是「界面上勾上了」。
   */
  test('勾选的分类跟着投稿一起提交', async () => {
    auth.me = { sub: 'x', username: 'u', displayName: 'U' };
    auth.isLoading = false;
    vi.mocked(submitDemo).mockResolvedValue({
      status: 201,
      data: { slug: 'demo-a', lifecycleState: 'draft', moderationState: 'pending' },
    } as never);

    renderAt('/submit');

    fireEvent.change(screen.getByLabelText('标题'), { target: { value: '我的作品' } });
    pickFile();

    const box = await screen.findByLabelText('小游戏');
    fireEvent.click(box);
    fireEvent.click(screen.getByLabelText('3D建模'));

    fireEvent.click(screen.getByRole('button', { name: '提交投稿' }));

    await waitFor(() => expect(submitDemo).toHaveBeenCalled());
    const [body] = vi.mocked(submitDemo).mock.calls[0] as unknown as [Record<string, unknown>];
    expect(body.classificationSlugs).toEqual(['game', '3D建模']);
  });

  /**
   * 手填的模型名也要进请求体。
   *
   * 这条钉的是「目录不是白名单」：灰测模型上游没收录，作者只能自己打。
   * 若手填的只留在草稿里、没进 value，投稿会成功但声明是空的——
   * 没有任何错误，作者也不会去核对。
   */
  test('手填的模型名跟着投稿一起提交', async () => {
    auth.me = { sub: 'x', username: 'u', displayName: 'U' };
    auth.isLoading = false;
    vi.mocked(submitDemo).mockResolvedValue({
      status: 201,
      data: { slug: 'demo-a', lifecycleState: 'draft', moderationState: 'pending' },
    } as never);

    renderAt('/submit');

    fireEvent.change(screen.getByLabelText('标题'), { target: { value: '用了灰测模型' } });
    pickFile();

    const box = await screen.findByLabelText('手填模型名');
    fireEvent.change(box, { target: { value: '某厂家的灰测模型' } });
    fireEvent.click(screen.getByRole('button', { name: '添加' }));

    fireEvent.click(screen.getByRole('button', { name: '提交投稿' }));

    await waitFor(() => expect(submitDemo).toHaveBeenCalled());
    const [body] = vi.mocked(submitDemo).mock.calls[0] as unknown as [Record<string, unknown>];
    expect(body.modelSlugs).toEqual(['某厂家的灰测模型']);
  });

  /**
   * 200 是重放（同一个 Idempotency-Key 已经建过），对用户来说是同一件事：
   * 作品在里面了。只认 201 的话，用户会看到一条空的报错。
   */
  test('重放（200）也当成提交成功', async () => {
    auth.me = { sub: 'x', username: 'u', displayName: 'U' };
    auth.isLoading = false;
    vi.mocked(submitDemo).mockResolvedValue({
      status: 200,
      data: { slug: 'demo-a', lifecycleState: 'draft', moderationState: 'pending' },
    } as never);

    renderAt('/submit');

    fireEvent.change(screen.getByLabelText('标题'), { target: { value: '我的作品' } });
    pickFile();
    fireEvent.click(screen.getByRole('button', { name: '提交投稿' }));

    await waitFor(() => expect(screen.getByText('投稿已提交')).toBeInTheDocument());
  });

  // 一个都没勾时不要塞一个空数组：契约里 modelIds 可省略，空数组和不传语义相同，
  // 但少一个字段能让请求体与日志都干净些。
  test('一个模型都没勾时不传 modelSlugs', async () => {
    auth.me = { sub: 'x', username: 'u', displayName: 'U' };
    auth.isLoading = false;
    vi.mocked(submitDemo).mockResolvedValue({
      status: 201,
      data: { slug: 'demo-a', lifecycleState: 'draft', moderationState: 'pending' },
    } as never);

    renderAt('/submit');
    fireEvent.change(screen.getByLabelText('标题'), { target: { value: '我的作品' } });
    pickFile();
    fireEvent.click(screen.getByRole('button', { name: '提交投稿' }));

    await waitFor(() => expect(submitDemo).toHaveBeenCalled());
    const [body] = vi.mocked(submitDemo).mock.calls[0] as unknown as [Record<string, unknown>];
    expect(body.modelSlugs).toBeUndefined();
  });
});

/**
 * 类型清单是主数据，页面不能自己写死一份。
 *
 * 这条钉的是「加了一个类型，投稿页还是只给两种」：界面选项必须来自清单，
 * 空清单的类型只能出现在「暂未开放」的提示里，不能进下拉。
 */
test('类型选项按目录渲染，暂未开放的类型只做提示', async () => {
  auth.me = { sub: 'x', username: 'u', displayName: 'U' };
  auth.isLoading = false;
  renderAt('/submit');

  const select = screen.getByLabelText('作品类型') as HTMLSelectElement;
  await waitFor(() =>
    expect([...select.options].map((o) => o.value)).toEqual([
      'web',
      'zip',
      'godot',
      'image',
      'model',
    ]),
  );
  expect(screen.getByText(/暂未开放投稿：外链/)).toBeInTheDocument();
  expect(select.options[3].textContent).toContain('图片');
  expect(select.options[3].textContent).toContain('PNG');
});

/**
 * 扩展名不符时本地就挡住。
 *
 * 服务端会拦（那是权威判据），但让作者等完一次上传再看到 400 是白等。
 * 断言落在「没发请求」上：只显示错误文案但照发请求，等于没挡住。
 */
test('选图片类型却传了 html，本地就挡住，不发请求', async () => {
  auth.me = { sub: 'x', username: 'u', displayName: 'U' };
  auth.isLoading = false;
  renderAt('/submit');

  fireEvent.change(screen.getByLabelText('标题'), { target: { value: '一张图' } });
  await screen.findByText(/暂未开放投稿/);
  fireEvent.change(screen.getByLabelText('作品类型'), { target: { value: 'image' } });
  pickFile();
  fireEvent.click(screen.getByRole('button', { name: '提交投稿' }));

  expect(await screen.findByText(/这个类型收 PNG \/ JPG \/ WebP/)).toBeInTheDocument();
  expect(submitDemo).not.toHaveBeenCalled();
});

/**
 * 目录读不到时不能把投稿页变成错误页。
 *
 * 退路只留最常用的两种：投稿这条路不能因为一个辅助清单挂了就整页不可用。
 */
test('类型目录读不到时退回两种常用类型并说明', async () => {
  auth.me = { sub: 'x', username: 'u', displayName: 'U' };
  auth.isLoading = false;
  listDemoTypesMock.mockResolvedValue({ status: 503, data: { code: 'unavailable' } });
  renderAt('/submit');

  expect(await screen.findByText(/类型目录暂时读不到/)).toBeInTheDocument();
  const select = screen.getByLabelText('作品类型') as HTMLSelectElement;
  expect([...select.options].map((o) => o.value)).toEqual(['web', 'zip']);
});
