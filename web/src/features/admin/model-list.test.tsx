import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { beforeEach, expect, test, vi } from 'vitest';

import { listAdminModels, type AdminModelRow } from '@/api';
import { ADMIN_MODEL_PAGE_SIZE, ModelList, type ModelListProps } from './model-list';

vi.mock('@/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/api')>();
  return {
    ...actual,
    listAdminModels: vi.fn(),
  };
});

function makeRow(
  overrides: Partial<AdminModelRow['model']> & { specs?: AdminModelRow['specs'] },
): AdminModelRow {
  const { specs = null, ...model } = overrides;
  return {
    model: {
      slug: 'model-a',
      name: 'Model A',
      vendor: 'DeepSeek',
      status: 'active',
      resolution: 'exact',
      mergedIntoSlug: '',
      source: 'models.dev',
      aliases: [],
      description: '',
      checkpoint: null,
      ...model,
    },
    specs,
  };
}

function renderList(props?: Partial<ModelListProps>) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const merged: ModelListProps = {
    status: '',
    page: 1,
    onStatusChange: vi.fn(),
    onPageChange: vi.fn(),
    ...props,
  };
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter>
        <ModelList {...merged} />
      </MemoryRouter>
    </QueryClientProvider>,
  );
  return merged;
}

beforeEach(() => {
  vi.mocked(listAdminModels).mockReset();
});

test('展示全部状态、厂商未标注、档案完整度三档与已下架 / 已合并标记', async () => {
  vi.mocked(listAdminModels).mockResolvedValue({
    status: 200,
    data: {
      items: [
        makeRow({
          slug: 'ds-active',
          name: 'DeepSeek Active',
          vendor: 'DeepSeek',
          status: 'active',
          specs: { contextWindow: 128000, modalitiesIn: 'text' },
        }),
        makeRow({
          slug: 'ds-cand',
          name: 'DeepSeek Cand',
          vendor: '',
          status: 'candidate',
          specs: null,
        }),
        makeRow({
          slug: 'ds-unver',
          name: 'DeepSeek Unver',
          vendor: 'OpenAI',
          status: 'unverified',
          specs: { contextWindow: 64000 },
        }),
        makeRow({
          slug: 'ds-merged',
          name: 'DeepSeek Merged',
          vendor: 'DeepSeek',
          status: 'deprecated',
          mergedIntoSlug: 'ds-active',
          specs: null,
        }),
        makeRow({
          slug: 'ds-delisted',
          name: 'DeepSeek Delisted',
          vendor: 'DeepSeek',
          status: 'deprecated',
          mergedIntoSlug: '',
          specs: null,
        }),
      ],
    },
  } as never);

  renderList();

  expect(await screen.findByText('DeepSeek Active')).toBeTruthy();
  expect(screen.getByText('DeepSeek Cand')).toBeTruthy();
  expect(screen.getByText('DeepSeek Unver')).toBeTruthy();
  expect(screen.getByText('DeepSeek Merged')).toBeTruthy();
  expect(screen.getByText('DeepSeek Delisted')).toBeTruthy();

  expect(screen.getByText('厂商未标注')).toBeTruthy();
  expect(screen.getByText('档案完整')).toBeTruthy();
  expect(screen.getByText('档案部分')).toBeTruthy();
  expect(screen.getAllByText('档案缺失')).toHaveLength(3);

  // 只有无归宿的已退役模型显示「已下架」，已合并模型显示合并归宿链接。
  expect(screen.getAllByText('已下架')).toHaveLength(1);
  expect(screen.getByRole('link', { name: 'ds-active' })).toBeTruthy();
});

test('切换状态筛选通知父组件并传参查询', async () => {
  vi.mocked(listAdminModels).mockResolvedValue({
    status: 200,
    data: { items: [makeRow({ slug: 'c-1', name: 'Cand 1', status: 'candidate' })] },
  } as never);

  const props = renderList({ status: 'candidate' });
  await waitFor(() =>
    expect(vi.mocked(listAdminModels)).toHaveBeenCalledWith({ status: 'candidate' }),
  );

  fireEvent.change(screen.getByLabelText('状态'), { target: { value: 'deprecated' } });
  expect(props.onStatusChange).toHaveBeenCalledWith('deprecated');
});

test('超过单页条数时按页切片并回调页码变化', async () => {
  const items = Array.from({ length: ADMIN_MODEL_PAGE_SIZE + 2 }, (_, i) =>
    makeRow({ slug: `m-${i + 1}`, name: `Model ${i + 1}` }),
  );
  vi.mocked(listAdminModels).mockResolvedValue({ status: 200, data: { items } } as never);

  const props = renderList({ page: 1 });
  expect(await screen.findByText('Model 1')).toBeTruthy();
  expect(screen.queryByText(`Model ${ADMIN_MODEL_PAGE_SIZE + 1}`)).toBeNull();

  fireEvent.click(screen.getByRole('button', { name: '下一页' }));
  expect(props.onPageChange).toHaveBeenCalledWith(2);
});

test('403 显示无查看权限说明，503 显示错误态与重试', async () => {
  vi.mocked(listAdminModels).mockResolvedValueOnce({ status: 403 } as never);
  renderList();
  expect(await screen.findByText('没有查看权限')).toBeTruthy();

  vi.mocked(listAdminModels).mockResolvedValueOnce({ status: 503 } as never);
  renderList({ status: 'active' });
  expect(await screen.findByText('模型列表加载失败')).toBeTruthy();
  expect(screen.getByRole('button', { name: '重试' })).toBeTruthy();
});
