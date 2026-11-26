import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, waitFor } from '@testing-library/react';
import { beforeEach, expect, test, vi } from 'vitest';

import { searchUpstreamModels, type UpstreamModel } from '@/api';
import { UpstreamModelSelector } from './upstream-model-selector';

vi.mock('@/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/api')>();
  return { ...actual, searchUpstreamModels: vi.fn() };
});

// 选择器的两条对外承诺：#589 的「已收录 → 站内 slug」与选中后把整行交给调用方。
// 字段不重算：slug 是服务端给的，前端只负责显示。

const claimed: UpstreamModel = {
  source: 'models.dev',
  providerId: 'deepseek',
  providerName: 'DeepSeek',
  externalId: 'deepseek-v4',
  name: 'DeepSeek V4',
  canonicalId: 'deepseek/deepseek-v4',
  upstreamStatus: 'stable',
  slug: 'deepseek-v4',
};

const unclaimed: UpstreamModel = {
  ...claimed,
  externalId: 'deepseek-v5-preview',
  name: 'DeepSeek V5 Preview',
  canonicalId: 'deepseek/deepseek-v5-preview',
  slug: null,
};

function renderSelector(items: UpstreamModel[], onSelect = vi.fn()) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  vi.mocked(searchUpstreamModels).mockResolvedValue({
    status: 200,
    data: { items },
  } as never);
  render(
    <QueryClientProvider client={client}>
      <UpstreamModelSelector
        q="deepseek"
        selectedRef={null}
        onSearch={() => {}}
        onSelect={onSelect}
      />
    </QueryClientProvider>,
  );
  return onSelect;
}

beforeEach(() => {
  vi.mocked(searchUpstreamModels).mockReset();
});

test('已收录的条目标出站内 slug', async () => {
  renderSelector([claimed]);
  await waitFor(() => expect(screen.getByText('已收录 → deepseek-v4')).toBeTruthy());
  expect(vi.mocked(searchUpstreamModels)).toHaveBeenCalledWith({ q: 'deepseek' });
});

test('未收录的条目标「未收录」，不显示 slug', async () => {
  renderSelector([unclaimed]);
  await waitFor(() => expect(screen.getByText('未收录')).toBeTruthy());
  // 「已收录 →」是徽标文案；页脚那句说明里也有「已收录」，不能拿它当判据。
  expect(screen.queryByText(/已收录 →/)).toBeNull();
});

test('点选条目把整行交给调用方', async () => {
  const onSelect = renderSelector([claimed, unclaimed]);
  await waitFor(() => expect(screen.getByText('DeepSeek V4')).toBeTruthy());
  screen.getByText('DeepSeek V4').click();
  expect(onSelect).toHaveBeenCalledTimes(1);
  expect(onSelect.mock.calls[0][0]).toMatchObject({
    providerId: 'deepseek',
    externalId: 'deepseek-v4',
    slug: 'deepseek-v4',
  });
});
