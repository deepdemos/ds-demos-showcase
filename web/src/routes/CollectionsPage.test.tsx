import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router';
import { beforeEach, describe, expect, test, vi } from 'vitest';
import { createCollection, listCollections } from '@/api';
import { CollectionsPage } from './CollectionsPage';

const auth = vi.hoisted(() => ({
  me: null as null | { sub: string; username: string },
}));

vi.mock('@/features/auth/AuthContext', () => ({
  useAuth: () => ({ ...auth, isLoading: false, isError: false, setMe: vi.fn(), signOut: vi.fn() }),
}));

vi.mock('@/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/api')>();
  return { ...actual, listCollections: vi.fn(), createCollection: vi.fn() };
});

const collection = (over: Record<string, unknown> = {}) => ({
  id: 1,
  name: '默认收藏',
  description: '',
  isDefault: true,
  visibility: 'private',
  itemCount: 0,
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z',
  ...over,
});

function renderPage() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={['/me/collections']}>
        <Routes>
          <Route path="/me/collections" element={<CollectionsPage />} />
          <Route path="/login" element={<div>登录页</div>} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  auth.me = { sub: 'u1', username: 'alice' };
  vi.mocked(listCollections).mockReset();
  vi.mocked(createCollection).mockReset();
});

describe('CollectionsPage', () => {
  test('列出默认夹与自定义夹，带件数与可见性', async () => {
    vi.mocked(listCollections).mockResolvedValue({
      status: 200,
      data: {
        items: [
          collection({ itemCount: 3 }),
          collection({
            id: 2,
            name: '灵感收藏',
            isDefault: false,
            visibility: 'public',
            itemCount: 7,
          }),
        ],
      },
    } as never);

    renderPage();

    expect(await screen.findByText('默认收藏')).toBeInTheDocument();
    expect(screen.getByText('灵感收藏')).toBeInTheDocument();
    expect(screen.getByText('3 件')).toBeInTheDocument();
    expect(screen.getByText('7 件')).toBeInTheDocument();
    expect(screen.getByText('公开')).toBeInTheDocument();
  });

  test('新建收藏夹会调 createCollection 并在成功后清空输入', async () => {
    vi.mocked(listCollections).mockResolvedValue({
      status: 200,
      data: { items: [collection()] },
    } as never);
    vi.mocked(createCollection).mockResolvedValue({
      status: 201,
      data: collection({ id: 2, name: '灵感', isDefault: false }),
    } as never);

    renderPage();
    const input = await screen.findByPlaceholderText('例如：灵感收藏');
    fireEvent.change(input, { target: { value: ' 灵感 ' } });
    fireEvent.click(screen.getByRole('button', { name: '新建' }));

    await waitFor(() => expect(createCollection).toHaveBeenCalledWith({ name: '灵感' }));
    await waitFor(() => expect(input).toHaveValue(''));
  });

  test('新建失败时展示后端文案', async () => {
    vi.mocked(listCollections).mockResolvedValue({
      status: 200,
      data: { items: [collection()] },
    } as never);
    vi.mocked(createCollection).mockResolvedValue({
      status: 422,
      data: { code: 'invalid_param', message: '已经有同名收藏夹' },
    } as never);

    renderPage();
    fireEvent.change(await screen.findByPlaceholderText('例如：灵感收藏'), {
      target: { value: '灵感' },
    });
    fireEvent.click(screen.getByRole('button', { name: '新建' }));

    expect(await screen.findByText('已经有同名收藏夹')).toBeInTheDocument();
  });

  test('未登录跳登录页', () => {
    auth.me = null;
    renderPage();
    expect(screen.getByText('登录页')).toBeInTheDocument();
    expect(listCollections).not.toHaveBeenCalled();
  });
});
