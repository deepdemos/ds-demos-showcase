import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { beforeEach, expect, test, vi } from 'vitest';

import { createModel, listModelVendors, suggestModelSlug } from '@/api';
import { ModelCreate } from './model-create';

/**
 * 手动新建模型表单（模型域 §4.7.3）。
 *
 * 钉的是「管理员看到什么、提交了什么、被拒之后有没有出路」：
 *
 * 1. slug 建议值由服务端给（前端不复制生成规则），填了名字就该出现；
 * 2. 手改过 slug 之后不再被建议值覆盖——那是管理员的意图；
 * 3. 建成了给出新建那条的链接；
 * 4. 撞名（409）时给已有那条的链接与「改用导入」的出路，不是一句「失败了」；
 * 5. 兜底名（400）直接展示服务端那句人话。
 */

vi.mock('@/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/api')>();
  return {
    ...actual,
    createModel: vi.fn(),
    suggestModelSlug: vi.fn(),
    listModelVendors: vi.fn(),
  };
});

function renderForm() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter>
        <ModelCreate />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

const created = {
  slug: 'deepseek-v4-1-flash',
  name: 'DeepSeek V4.1 Flash',
  vendor: 'deepseek',
  status: 'active',
  resolution: 'exact',
  mergedIntoSlug: '',
  source: 'manual',
  aliases: [],
  description: '',
  checkpoint: null,
};

// 厂商下拉的默认数据源：一家已有的厂商。用例要改这份就自己 mockResolvedValue。
const vendorItems = {
  items: [{ id: 7, slug: 'deepseek', name: 'DeepSeek' }],
};

beforeEach(() => {
  vi.mocked(createModel).mockReset();
  vi.mocked(suggestModelSlug).mockReset();
  vi.mocked(listModelVendors).mockReset();
  vi.mocked(suggestModelSlug).mockResolvedValue({
    status: 200,
    data: { slug: 'deepseek-v4-1-flash' },
  } as never);
  vi.mocked(listModelVendors).mockResolvedValue({ status: 200, data: vendorItems } as never);
});

test('填上名字就给出 slug 建议值（服务端算，前端不复制规则）', async () => {
  renderForm();
  fireEvent.change(screen.getByLabelText('模型名'), { target: { value: 'DeepSeek V4.1 Flash' } });

  await waitFor(() =>
    expect(vi.mocked(suggestModelSlug)).toHaveBeenCalledWith({
      name: 'DeepSeek V4.1 Flash',
      vendor: undefined,
    }),
  );
  await waitFor(() =>
    expect((screen.getByLabelText(/^slug/) as HTMLInputElement).value).toBe('deepseek-v4-1-flash'),
  );
});

test('手改过 slug 之后不再被建议值覆盖（连请求都不再发）', async () => {
  renderForm();
  fireEvent.change(screen.getByLabelText('模型名'), { target: { value: 'DeepSeek V4.1 Flash' } });
  const slugInput = screen.getByLabelText(/^slug/) as HTMLInputElement;
  await waitFor(() => expect(slugInput.value).toBe('deepseek-v4-1-flash'));
  const callsBefore = vi.mocked(suggestModelSlug).mock.calls.length;

  fireEvent.change(slugInput, { target: { value: 'my-own-slug' } });
  // 再改名字：手填的 slug 是管理员的意图，不该被建议值冲掉——连拉都不该拉。
  fireEvent.change(screen.getByLabelText('模型名'), { target: { value: 'DeepSeek V4.1 Flash 2' } });
  await new Promise((resolve) => window.setTimeout(resolve, 500));

  expect(slugInput.value).toBe('my-own-slug');
  expect(vi.mocked(suggestModelSlug).mock.calls.length).toBe(callsBefore);
});

test('建成后给出新建那条模型的链接', async () => {
  vi.mocked(createModel).mockResolvedValue({ status: 201, data: created } as never);
  renderForm();

  fireEvent.change(screen.getByLabelText('模型名'), { target: { value: 'DeepSeek V4.1 Flash' } });
  await waitFor(() =>
    expect((screen.getByLabelText(/^slug/) as HTMLInputElement).value).toBe('deepseek-v4-1-flash'),
  );
  fireEvent.click(screen.getByRole('button', { name: '新建模型' }));

  await waitFor(() =>
    expect(vi.mocked(createModel)).toHaveBeenCalledWith({
      name: 'DeepSeek V4.1 Flash',
      slug: 'deepseek-v4-1-flash',
    }),
  );
  const link = (await screen.findByRole('link', {
    name: 'DeepSeek V4.1 Flash',
  })) as HTMLAnchorElement;
  expect(link.getAttribute('href')).toBe('/models/deepseek-v4-1-flash');
});

test('撞名时给出已有那条的链接与改用导入的提示', async () => {
  vi.mocked(createModel).mockResolvedValue({
    status: 409,
    data: {
      code: 'conflict',
      message: '已经有同名模型「DeepSeek V4.1 Flash」（/models/deepseek-v4-1-flash）。',
      existingSlug: 'deepseek-v4-1-flash',
    },
  } as never);
  renderForm();

  fireEvent.change(screen.getByLabelText('模型名'), { target: { value: 'DeepSeek V4.1 Flash' } });
  fireEvent.click(screen.getByRole('button', { name: '新建模型' }));

  const link = (await screen.findByRole('link', {
    name: 'deepseek-v4-1-flash',
  })) as HTMLAnchorElement;
  expect(link.getAttribute('href')).toBe('/models/deepseek-v4-1-flash');
  expect(screen.getByRole('alert').textContent).toMatch(/导入/);
});

test('兜底名被拒时展示服务端那句说明', async () => {
  vi.mocked(createModel).mockResolvedValue({
    status: 400,
    data: {
      code: 'invalid_param',
      message:
        '「unspecified」是系统维护的兜底名（表示说不清具体型号），不能手动新建；要收录真实型号请走导入',
    },
  } as never);
  renderForm();

  fireEvent.change(screen.getByLabelText('模型名'), { target: { value: 'unspecified' } });
  fireEvent.click(screen.getByRole('button', { name: '新建模型' }));

  const alert = await screen.findByRole('alert');
  expect(alert.textContent).toMatch(/兜底名/);
});

test('没有 model.create 权限时说清缺哪个键', async () => {
  vi.mocked(createModel).mockResolvedValue({ status: 403 } as never);
  renderForm();

  fireEvent.change(screen.getByLabelText('模型名'), { target: { value: 'DeepSeek V6' } });
  fireEvent.click(screen.getByRole('button', { name: '新建模型' }));

  const alert = await screen.findByRole('alert');
  expect(alert.textContent).toMatch(/model\.create/);
});
test('选中已有厂商时提交 vendorId，slug 建议值用那家厂商的名字', async () => {
  vi.mocked(createModel).mockResolvedValue({ status: 201, data: created } as never);
  renderForm();

  fireEvent.change(screen.getByLabelText('模型名'), { target: { value: 'DeepSeek V4.1 Flash' } });
  // 下拉里要有服务端读回来的那一家——否则「选已有」这条路口根本不存在。
  await screen.findByRole('option', { name: 'DeepSeek' });
  fireEvent.change(screen.getByLabelText(/^厂商/), { target: { value: '7' } });

  await waitFor(() =>
    expect(vi.mocked(suggestModelSlug)).toHaveBeenCalledWith({
      name: 'DeepSeek V4.1 Flash',
      vendor: 'DeepSeek',
    }),
  );
  await waitFor(() =>
    expect((screen.getByLabelText(/^slug/) as HTMLInputElement).value).toBe('deepseek-v4-1-flash'),
  );

  fireEvent.click(screen.getByRole('button', { name: '新建模型' }));
  await waitFor(() =>
    expect(vi.mocked(createModel)).toHaveBeenCalledWith({
      name: 'DeepSeek V4.1 Flash',
      slug: 'deepseek-v4-1-flash',
      vendorId: 7,
    }),
  );
});

test('选「新建一家厂商」并填名字时提交 vendor 名称，不带 vendorId', async () => {
  vi.mocked(createModel).mockResolvedValue({ status: 201, data: created } as never);
  renderForm();

  fireEvent.change(screen.getByLabelText('模型名'), { target: { value: 'GLM-5' } });
  await screen.findByRole('option', { name: 'DeepSeek' });
  fireEvent.change(screen.getByLabelText(/^厂商/), { target: { value: '__new__' } });

  fireEvent.change(await screen.findByLabelText('新厂商名'), { target: { value: '智谱' } });
  // 建议值跟着新厂商名走：不填 slug 时服务端会用这个名字算。
  await waitFor(() =>
    expect(vi.mocked(suggestModelSlug)).toHaveBeenCalledWith({
      name: 'GLM-5',
      vendor: '智谱',
    }),
  );

  fireEvent.click(screen.getByRole('button', { name: '新建模型' }));
  await waitFor(() => expect(vi.mocked(createModel)).toHaveBeenCalled());
  // 经 unknown 转一次：契约类型没有索引签名，直接转 Record 会被 tsc 拦。
  const body = vi.mocked(createModel).mock.calls[0][0] as unknown as Record<string, unknown>;
  expect(body.vendor).toBe('智谱');
  expect(body).not.toHaveProperty('vendorId');
});

test('厂商下拉读不到时仍能建模型（厂商可留空）', async () => {
  vi.mocked(listModelVendors).mockResolvedValue({ status: 403 } as never);
  vi.mocked(createModel).mockResolvedValue({ status: 201, data: created } as never);
  renderForm();

  // 说清「读不到」，并给一条不依赖下拉的出路。
  expect(await screen.findByText(/厂商下拉暂时读不到/)).toBeInTheDocument();

  fireEvent.change(screen.getByLabelText('模型名'), { target: { value: 'DeepSeek V4.1 Flash' } });
  await waitFor(() =>
    expect((screen.getByLabelText(/^slug/) as HTMLInputElement).value).toBe('deepseek-v4-1-flash'),
  );
  fireEvent.click(screen.getByRole('button', { name: '新建模型' }));

  await waitFor(() =>
    expect(vi.mocked(createModel)).toHaveBeenCalledWith({
      name: 'DeepSeek V4.1 Flash',
      slug: 'deepseek-v4-1-flash',
    }),
  );
});

test('厂商名撞 slug（409 且没有 existingSlug）时只展示文案，不给模型链接', async () => {
  vi.mocked(createModel).mockResolvedValue({
    status: 409,
    data: {
      code: 'conflict',
      message: '厂商名折出的 slug「zhipu」已经被「智谱」占用，请换个厂商名，或直接选那一家',
    },
  } as never);
  renderForm();

  fireEvent.change(screen.getByLabelText('模型名'), { target: { value: 'GLM-5' } });
  await screen.findByRole('option', { name: 'DeepSeek' });
  fireEvent.change(screen.getByLabelText(/^厂商/), { target: { value: '__new__' } });
  fireEvent.change(await screen.findByLabelText('新厂商名'), { target: { value: 'ZhiPu' } });
  fireEvent.click(screen.getByRole('button', { name: '新建模型' }));

  const alert = await screen.findByRole('alert');
  expect(alert.textContent).toMatch(/换个厂商名/);
  // 撞的是厂商，不是模型：给「已有那条模型」的链接会把人带到错的地方。
  expect(screen.queryByRole('link')).toBeNull();
});
