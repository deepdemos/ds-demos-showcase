import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes, useLocation } from 'react-router';
import { beforeEach, expect, test, vi } from 'vitest';

import {
  applyModelImportBatch,
  getModelSyncStatus,
  listAdminModels,
  listModelVendors,
  previewModelImport,
  searchUpstreamModels,
  syncModelCatalog,
} from '@/api';
import { AdminModelsPage } from './AdminModelsPage';

const auth = vi.hoisted(() => ({
  me: {
    id: 1,
    username: 'admin',
    displayName: '管理员',
    role: 'admin',
    permissions: ['model.import', 'model.create'],
  } as {
    id: number;
    username: string;
    displayName: string;
    role: string;
    permissions: string[];
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
    searchUpstreamModels: vi.fn(),
    previewModelImport: vi.fn(),
    applyModelImportBatch: vi.fn(),
    listAdminModels: vi.fn(),
    listModelVendors: vi.fn(),
  };
});

// 选择器的行形状（公开上游搜索的响应）。imported 那个布尔被 slug 取代了：
// 已收录时给商店内模型 slug，未收录是 null。
const item = {
  source: 'models.dev',
  providerId: 'deepseek',
  providerName: 'DeepSeek',
  externalId: 'deepseek-flash',
  name: 'DeepSeek V4.1 Flash',
  canonicalId: 'deepseek/deepseek-v4.1-flash',
  upstreamStatus: 'stable',
  slug: null,
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

function LocationProbe() {
  const location = useLocation();
  return <output data-testid="loc-search">{location.search}</output>;
}

function renderPage(entry = '/admin/models') {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[entry]}>
        <Routes>
          <Route
            path="/admin/models"
            element={
              <>
                <AdminModelsPage />
                <LocationProbe />
              </>
            }
          />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  auth.me = {
    id: 1,
    username: 'admin',
    displayName: '管理员',
    role: 'admin',
    permissions: ['model.import', 'model.create'],
  };
  auth.isLoading = false;
  vi.mocked(getModelSyncStatus).mockResolvedValue({ status: 200, data: statusOk } as never);
  vi.mocked(searchUpstreamModels).mockResolvedValue({
    status: 200,
    data: { items: [item] },
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
  vi.mocked(listAdminModels).mockResolvedValue({
    status: 200,
    data: { items: [] },
  } as never);
  vi.mocked(listModelVendors).mockResolvedValue({
    status: 200,
    data: { items: [] },
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

// 非 admin 且无模型权限看到的是说明而不是重定向：重定向会让人以为链接坏了。
test('无模型管理权限的账号看到「没有导入权限」', async () => {
  auth.me = { id: 2, username: 'u', displayName: '普通用户', role: 'user', permissions: [] };
  renderPage();
  await waitFor(() => expect(screen.getByText('没有导入权限')).toBeTruthy());
  expect(vi.mocked(searchUpstreamModels)).not.toHaveBeenCalled();
  expect(vi.mocked(listAdminModels)).not.toHaveBeenCalled();
});

// 持有 model.create（非 admin 角色）也能进入模型页看到模型列表，与后端 requireAnyPermission 口径一致。
test('非 admin 但持有 model.create 权限的账号可进入并加载模型列表', async () => {
  auth.me = {
    id: 3,
    username: 'creator',
    displayName: '模型建档员',
    role: 'custom-model-creator',
    permissions: ['model.create'],
  };
  vi.mocked(listAdminModels).mockResolvedValue({
    status: 200,
    data: {
      items: [
        {
          model: {
            slug: 'perm-ok-model',
            name: 'Perm OK Model',
            vendor: 'DeepSeek',
            status: 'active',
            resolution: 'exact',
            mergedIntoSlug: '',
            source: 'manual',
            aliases: [],
            description: '',
            checkpoint: null,
          },
          specs: null,
        },
      ],
    },
  } as never);

  renderPage();
  expect(await screen.findByText('Perm OK Model')).toBeTruthy();
  expect(vi.mocked(listAdminModels)).toHaveBeenCalledWith(undefined);
});

// 目录读失败必须有出路：错误态带重试，而不是留一块空白。
test('目录读取失败时给出重试', async () => {
  vi.mocked(searchUpstreamModels).mockResolvedValue({ status: 503 } as never);
  renderPage();
  await waitFor(() => expect(screen.getByText('目录读取失败')).toBeTruthy());
  expect(screen.getByText('重试')).toBeTruthy();
});

// #596：筛选与分页参数进 URL，刷新可复原，切换状态自动回第一页。
test('模型列表状态筛选与分页同步到 URL 并可从 URL 复原', async () => {
  const items = Array.from({ length: 22 }, (_, i) => ({
    model: {
      slug: `cand-${i + 1}`,
      name: `Candidate ${i + 1}`,
      vendor: 'DeepSeek',
      status: 'candidate' as const,
      resolution: 'exact' as const,
      mergedIntoSlug: '',
      source: 'manual' as const,
      aliases: [],
      description: '',
    },
    specs: null,
  }));
  vi.mocked(listAdminModels).mockResolvedValue({ status: 200, data: { items } } as never);

  renderPage('/admin/models?status=candidate&page=2');
  await waitFor(() =>
    expect(vi.mocked(listAdminModels)).toHaveBeenCalledWith({ status: 'candidate' }),
  );
  expect(await screen.findByText('Candidate 21')).toBeTruthy();

  fireEvent.change(screen.getByLabelText('状态'), { target: { value: 'deprecated' } });
  await waitFor(() =>
    expect(screen.getByTestId('loc-search').textContent).toBe('?status=deprecated'),
  );
});

test('非法 status 与非有限 page 安全回退，翻页时清理脏 status 参数', async () => {
  const items = Array.from({ length: 22 }, (_, i) => ({
    model: {
      slug: `m-${i + 1}`,
      name: `Model ${i + 1}`,
      vendor: 'DeepSeek',
      status: 'active' as const,
      resolution: 'exact' as const,
      mergedIntoSlug: '',
      source: 'manual' as const,
      aliases: [],
      description: '',
      checkpoint: null,
    },
    specs: null,
  }));
  vi.mocked(listAdminModels).mockResolvedValue({ status: 200, data: { items } } as never);

  renderPage('/admin/models?status=bogus&page=Infinity');
  await waitFor(() => expect(vi.mocked(listAdminModels)).toHaveBeenCalledWith(undefined));
  expect(await screen.findByText('Model 1')).toBeTruthy();

  fireEvent.click(screen.getByRole('button', { name: '下一页' }));
  await waitFor(() => expect(screen.getByTestId('loc-search').textContent).toBe('?page=2'));
});
