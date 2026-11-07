import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router';
import { beforeEach, expect, test, vi } from 'vitest';

import {
  applyModelImportBatch,
  getModelSyncStatus,
  listModelCatalog,
  previewModelImport,
  syncModelCatalog,
} from '@/api';
import { AdminModelsPage } from './AdminModelsPage';

const auth = vi.hoisted(() => ({
  me: { id: 1, username: 'admin', displayName: '管理员', role: 'admin' } as {
    id: number;
    username: string;
    displayName: string;
    role: string;
  } | null,
  isLoading: false,
}));

vi.mock('@/features/auth/AuthContext', () => ({
  useAuth: () => ({ me: auth.me, isLoading: auth.isLoading }),
}));

vi.mock('@/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/api')>();
  return {
    ...actual,
    getModelSyncStatus: vi.fn(),
    syncModelCatalog: vi.fn(),
    listModelCatalog: vi.fn(),
    previewModelImport: vi.fn(),
    applyModelImportBatch: vi.fn(),
  };
});

const item = {
  providerId: 'deepseek',
  providerName: 'DeepSeek',
  externalId: 'deepseek-flash',
  name: 'DeepSeek V4.1 Flash',
  canonicalId: 'deepseek/deepseek-v4.1-flash',
  vendorSlug: 'deepseek',
  family: '',
  modelType: 'chat',
  upstreamStatus: 'stable',
  imported: false,
  contextWindow: 1048576,
  maxOutput: null,
  costIn: null,
  costOut: null,
  releaseDate: null,
  modalityIn: null,
  modalityOut: null,
  description: null,
};

const statusOk = {
  source: 'models.dev',
  status: 'ok',
  items: 8931,
  lastAttemptAt: '2026-11-07T22:00:00Z',
  lastSuccessAt: '2026-11-07T22:00:00Z',
  error: null,
};

const previewCreate = {
  mode: 'create',
  fields: [
    {
      field: 'name',
      current: '',
      upstream: 'DeepSeek V4.1 Flash',
      source: 'none',
      locked: false,
      selectable: true,
    },
    {
      field: 'contextWindow',
      current: '',
      upstream: '1048576',
      source: 'none',
      locked: false,
      selectable: true,
    },
    {
      field: 'description',
      current: '人工写的简介',
      upstream: '上游营销话术',
      source: 'manual',
      locked: false,
      selectable: false,
    },
  ],
};

function renderPage(entry = '/admin/models') {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[entry]}>
        <Routes>
          <Route path="/admin/models" element={<AdminModelsPage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  auth.me = { id: 1, username: 'admin', displayName: '管理员', role: 'admin' };
  auth.isLoading = false;
  vi.mocked(getModelSyncStatus).mockResolvedValue({ status: 200, data: statusOk } as never);
  vi.mocked(listModelCatalog).mockResolvedValue({
    status: 200,
    data: { items: [item], total: 1, limit: 50, offset: 0 },
  } as never);
  vi.mocked(previewModelImport).mockResolvedValue({
    status: 200,
    data: previewCreate,
  } as never);
  vi.mocked(syncModelCatalog).mockResolvedValue({
    status: 200,
    data: {
      source: 'models.dev',
      status: 'ok',
      items: 8931,
      skipped: true,
      syncedAt: '2026-11-07T22:00:00Z',
    },
  } as never);
  vi.mocked(applyModelImportBatch).mockResolvedValue({
    status: 200,
    data: {
      results: [
        {
          providerId: 'deepseek',
          externalId: 'deepseek-flash',
          status: 'created',
          model: { slug: 'deepseek-v4-1-flash' },
        },
      ],
    },
  } as never);
});

// 顶部条读的是「记账」，不是「刚才那一次」——刷新之后也得知道镜像有多旧。
test('顶部条显示同步状态与条数', async () => {
  renderPage();
  await waitFor(() => expect(screen.getByText('已同步')).toBeTruthy());
  expect(screen.getByText(/8,931 条/)).toBeTruthy();
});

// 从未同步过是初始状态，不是故障。
test('从未同步过时说的是「从未同步过」，不是错误', async () => {
  vi.mocked(getModelSyncStatus).mockResolvedValue({
    status: 200,
    data: {
      source: 'models.dev',
      status: 'never',
      items: 0,
      lastAttemptAt: null,
      lastSuccessAt: null,
      error: null,
    },
  } as never);
  renderPage();
  await waitFor(() => expect(screen.getByText('从未同步过')).toBeTruthy());
  expect(screen.queryByRole('alert')).toBeNull();
});

// 点「立即同步」要真的发请求，而不是只换个文案。
test('点立即同步会调用同步端点', async () => {
  renderPage();
  await waitFor(() => expect(screen.getByText('立即同步')).toBeTruthy());
  screen.getByText('立即同步').click();
  await waitFor(() => expect(vi.mocked(syncModelCatalog)).toHaveBeenCalled());
  await waitFor(() => expect(screen.getByText(/上游内容没有变化/)).toBeTruthy());
});

// 左侧条目点选后走 preview：不打这个请求，右侧就永远是空态。
test('点选目录条目会拉差异并显示上游值', async () => {
  renderPage();
  await waitFor(() => expect(screen.getByText('DeepSeek V4.1 Flash')).toBeTruthy());
  screen.getByText('DeepSeek V4.1 Flash').click();
  await waitFor(() =>
    expect(vi.mocked(previewModelImport)).toHaveBeenCalledWith({
      providerId: 'deepseek',
      externalId: 'deepseek-flash',
      canonicalId: 'deepseek/deepseek-v4.1-flash',
      source: 'models.dev',
    }),
  );
  await waitFor(() => expect(screen.getByText('1048576')).toBeTruthy());
});

// 人工写过的字段不可勾，并且要说清原因——否则管理员会以为实现漏了。
test('人工写过的字段不可勾，并写明原因', async () => {
  renderPage('/admin/models?ref=deepseek/deepseek-flash');
  await waitFor(() => expect(screen.getByText(/人工写过：同步不覆盖人工内容/)).toBeTruthy());
  const box = screen.getByRole('checkbox', { name: '选择 description' }) as HTMLInputElement;
  expect(box.disabled).toBe(true);
});

// 加入清单 → 批量导入 → 逐条结果。三段里断一段，这条路就走不完。
test('加入待导入后能批量提交并看到逐条结果', async () => {
  renderPage('/admin/models?ref=deepseek/deepseek-flash');
  await waitFor(() => expect(screen.getByText('加入待导入')).toBeTruthy());
  screen.getByText('加入待导入').click();
  await waitFor(() => expect(screen.getByText('批量导入')).toBeTruthy());
  screen.getByText('批量导入').click();
  await waitFor(() => expect(vi.mocked(applyModelImportBatch)).toHaveBeenCalled());
  const body = vi.mocked(applyModelImportBatch).mock.calls[0][0];
  expect(body.items[0].providerId).toBe('deepseek');
  expect(body.items[0].fields).toEqual(['name', 'contextWindow']);
  await waitFor(() => expect(screen.getByText(/成功 1 条/)).toBeTruthy());
});

// 非 admin 看到的是说明而不是重定向：重定向会让人以为链接坏了。
test('非 admin 看到「没有导入权限」', async () => {
  auth.me = { id: 2, username: 'u', displayName: '普通用户', role: 'user' };
  renderPage();
  await waitFor(() => expect(screen.getByText('没有导入权限')).toBeTruthy());
  expect(vi.mocked(listModelCatalog)).not.toHaveBeenCalled();
});

// 目录读失败必须有出路：错误态带重试，而不是留一块空白。
test('目录读取失败时给出重试', async () => {
  vi.mocked(listModelCatalog).mockResolvedValue({ status: 503 } as never);
  renderPage();
  await waitFor(() => expect(screen.getByText('目录读取失败')).toBeTruthy());
  expect(screen.getByText('重试')).toBeTruthy();
});
