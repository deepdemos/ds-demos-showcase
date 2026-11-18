import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router';
import { beforeEach, describe, expect, test, vi } from 'vitest';
import {
  deleteCollection,
  getCollection,
  listCollections,
  removeCollectionItem,
  updateCollection,
} from '@/api';
import { CollectionDetailPage } from './CollectionDetailPage';

const auth = vi.hoisted(() => ({
  me: null as null | { sub: string; username: string },
}));

vi.mock('@/features/auth/AuthContext', () => ({
  useAuth: () => ({ ...auth, isLoading: false, isError: false, setMe: vi.fn(), signOut: vi.fn() }),
}));

vi.mock('@/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/api')>();
  return {
    ...actual,
    getCollection: vi.fn(),
    listCollections: vi.fn(),
    updateCollection: vi.fn(),
    deleteCollection: vi.fn(),
    removeCollectionItem: vi.fn(),
  };
});

const collection = (over: Record<string, unknown> = {}) => ({
  id: 1,
  name: '灵感收藏',
  description: '攒点灵感',
  isDefault: false,
  visibility: 'private',
  itemCount: 2,
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z',
  ...over,
});

const item = (slug: string, title: string, accessible: boolean) => ({
  slug,
  title,
  coverUrl: null,
  addedAt: '2026-01-01T00:00:00Z',
  accessible,
});

function renderPage() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={['/me/collections/1']}>
        <Routes>
          <Route path="/me/collections" element={<div>列表页</div>} />
          <Route path="/me/collections/:id" element={<CollectionDetailPage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

function seedOwner() {
  vi.mocked(listCollections).mockResolvedValue({
    status: 200,
    data: { items: [collection()] },
  } as never);
}

beforeEach(() => {
  auth.me = { sub: 'u1', username: 'alice' };
  vi.mocked(getCollection).mockReset();
  vi.mocked(listCollections).mockReset();
  vi.mocked(updateCollection).mockReset();
  vi.mocked(deleteCollection).mockReset();
  vi.mocked(removeCollectionItem).mockReset();
});

describe('CollectionDetailPage', () => {
  test('主人看到管理区、条目与墓碑条目', async () => {
    seedOwner();
    vi.mocked(getCollection).mockResolvedValue({
      status: 200,
      data: {
        collection: collection(),
        items: [item('demo-a', '一号', true), item('demo-b', '已删作品', false)],
      },
    } as never);

    const { container } = renderPage();

    expect(await screen.findByRole('heading', { name: /灵感收藏/ })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: '一号' })).toHaveAttribute('href', '/demos/demo-a');
    expect(screen.getByText('该作品已不可访问')).toBeInTheDocument();
    expect(container.querySelector('[data-slug="demo-b"] a')).toBeNull();
    expect(screen.getByRole('button', { name: '保存' })).toBeInTheDocument();
  });

  test('保存改名会调 updateCollection', async () => {
    seedOwner();
    vi.mocked(getCollection).mockResolvedValue({
      status: 200,
      data: { collection: collection(), items: [] },
    } as never);
    vi.mocked(updateCollection).mockResolvedValue({
      status: 200,
      data: collection({ name: '新名字' }),
    } as never);

    renderPage();
    const input = await screen.findByDisplayValue('灵感收藏');
    fireEvent.change(input, { target: { value: '新名字' } });
    fireEvent.click(screen.getByRole('button', { name: '保存' }));

    await waitFor(() =>
      expect(updateCollection).toHaveBeenCalledWith(1, {
        name: '新名字',
        description: '攒点灵感',
        visibility: 'private',
      }),
    );
  });

  test('移出条目与删除收藏夹都要显式动作', async () => {
    seedOwner();
    vi.mocked(getCollection).mockResolvedValue({
      status: 200,
      data: { collection: collection(), items: [item('demo-a', '一号', true)] },
    } as never);
    vi.mocked(removeCollectionItem).mockResolvedValue({ status: 204, data: undefined } as never);
    vi.mocked(deleteCollection).mockResolvedValue({ status: 204, data: undefined } as never);

    renderPage();
    fireEvent.click(await screen.findByRole('button', { name: '移出 一号' }));
    await waitFor(() => expect(removeCollectionItem).toHaveBeenCalledWith(1, 'demo-a'));

    fireEvent.click(screen.getByRole('button', { name: '删除收藏夹' }));
    expect(screen.getByText(/删除收藏夹不会删除作品/)).toBeInTheDocument();
    expect(deleteCollection).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole('button', { name: '确认删除' }));
    await waitFor(() => expect(deleteCollection).toHaveBeenCalledWith(1));
    expect(await screen.findByText('列表页')).toBeInTheDocument();
  });

  test('非主人看公开夹：没有管理区', async () => {
    auth.me = null;
    vi.mocked(getCollection).mockResolvedValue({
      status: 200,
      data: { collection: collection({ visibility: 'public' }), items: [] },
    } as never);

    renderPage();

    expect(await screen.findByRole('heading', { name: /灵感收藏/ })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: '保存' })).toBeNull();
    expect(screen.queryByRole('button', { name: '删除收藏夹' })).toBeNull();
  });

  test('私有夹 / 不存在：落「不存在或不可见」', async () => {
    vi.mocked(getCollection).mockResolvedValue({
      status: 404,
      data: { code: 'not_found', message: '收藏夹不存在' },
    } as never);

    renderPage();

    expect(await screen.findByText('收藏夹不存在或不可见')).toBeInTheDocument();
  });
});
