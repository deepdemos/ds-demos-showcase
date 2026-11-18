import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { beforeEach, describe, expect, test, vi } from 'vitest';
import { getCollectionStatus, listCollections } from '@/api';
import { CollectionControls } from './collection-controls';

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
    getCollectionStatus: vi.fn(),
    listCollections: vi.fn(),
    addCollectionItem: vi.fn(),
    removeCollectionItem: vi.fn(),
  };
});

function renderControl() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter>
        <CollectionControls slug="demo-a" />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  auth.me = { sub: 'u1', username: 'alice' };
  vi.mocked(getCollectionStatus).mockReset();
  vi.mocked(listCollections).mockReset();
});

describe('CollectionControls', () => {
  test('收藏夹读取失败时不谎称「还没有自定义收藏夹」，并给重试', async () => {
    vi.mocked(getCollectionStatus).mockResolvedValue({
      status: 200,
      data: { inDefault: false, collectionIds: [] },
    } as never);
    vi.mocked(listCollections).mockResolvedValue({ status: 500, data: {} } as never);

    renderControl();

    expect(await screen.findByText('收藏状态加载失败')).toBeInTheDocument();
    expect(screen.queryByText(/还没有自定义收藏夹/)).toBeNull();
    expect(screen.queryByRole('button', { name: '收藏' })).toBeNull();

    fireEvent.click(screen.getByRole('button', { name: /重试/ }));
    await waitFor(() => expect(listCollections).toHaveBeenCalledTimes(2));
  });
});
