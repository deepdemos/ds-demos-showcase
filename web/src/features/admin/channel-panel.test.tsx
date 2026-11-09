import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, test, vi } from 'vitest';
import { addDemoChannel, listDemoChannels, removeDemoChannel } from '@/api';
import { ChannelPanel } from '@/features/admin/channel-panel';

vi.mock('@/api', () => ({
  listDemoChannels: vi.fn(),
  addDemoChannel: vi.fn(),
  removeDemoChannel: vi.fn(),
}));

function renderPanel() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <ChannelPanel slug="demo-a" />
    </QueryClientProvider>,
  );
}

const twoChannels = {
  status: 200,
  data: {
    items: [
      { channel: 'astra', visibility: 'unlisted', lang: 'en' },
      { channel: 'deep', visibility: 'listed', lang: 'zh' },
    ],
  },
};

describe('ChannelPanel', () => {
  beforeEach(() => {
    vi.mocked(listDemoChannels).mockReset();
    vi.mocked(addDemoChannel).mockReset();
    vi.mocked(removeDemoChannel).mockReset();
  });

  test('列出已有渠道，并说明最后一个不能撤', async () => {
    vi.mocked(listDemoChannels).mockResolvedValue({
      status: 200,
      data: { items: [{ channel: 'deep', visibility: 'listed', lang: 'zh' }] },
    } as never);

    renderPanel();
    // 等数据到位：文案在 isPending 时不出现。
    expect(await screen.findByText('公开列出 · zh')).toBeInTheDocument();
    expect(screen.getByText('最后一个渠道不能撤')).toBeInTheDocument();
    // 只剩 deep 时，Astra 应该是唯一可追加的选项。
    expect(screen.getByRole('button', { name: '上架到 Astra 橱窗' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: '上架到 主站' })).not.toBeInTheDocument();
  });

  test('上架 Astra 时自动带 lang=en（渠道属性，不是用户选择）', async () => {
    vi.mocked(listDemoChannels).mockResolvedValue({
      status: 200,
      data: { items: [{ channel: 'deep', visibility: 'listed', lang: 'zh' }] },
    } as never);
    vi.mocked(addDemoChannel).mockResolvedValue({ status: 201, data: {} } as never);

    renderPanel();
    fireEvent.click(await screen.findByRole('button', { name: '上架到 Astra 橱窗' }));

    await waitFor(() =>
      expect(vi.mocked(addDemoChannel)).toHaveBeenCalledWith('demo-a', {
        channel: 'astra',
        visibility: 'unlisted',
        lang: 'en',
      }),
    );
  });

  test('有两个渠道时可以撤下其中一个', async () => {
    vi.mocked(listDemoChannels).mockResolvedValue(twoChannels as never);
    vi.mocked(removeDemoChannel).mockResolvedValue({ status: 204, data: undefined } as never);

    renderPanel();
    // 两行各一个「撤下」；撤 astra 那一行（列表按渠道名排序，astra 在第一个）。
    const buttons = await screen.findAllByRole('button', { name: '撤下' });
    expect(buttons).toHaveLength(2);
    fireEvent.click(buttons[0]);

    await waitFor(() =>
      expect(vi.mocked(removeDemoChannel)).toHaveBeenCalledWith('demo-a', 'astra'),
    );
  });

  test('没有渠道时说清楚，而不是画一张空列表', async () => {
    vi.mocked(listDemoChannels).mockResolvedValue({ status: 200, data: { items: [] } } as never);

    renderPanel();
    expect(await screen.findByText('还没有渠道')).toBeInTheDocument();
  });

  test('读不到给重试，不是静默空白', async () => {
    vi.mocked(listDemoChannels).mockResolvedValue({ status: 503, data: {} } as never);

    renderPanel();
    expect(await screen.findByText('渠道读不到')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /重试/ })).toBeInTheDocument();
  });
});
