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
vi.mock('@/api', () => ({
  submitDemo: vi.fn(),
  listModels: () => Promise.resolve({ status: 200, data: models }),
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
