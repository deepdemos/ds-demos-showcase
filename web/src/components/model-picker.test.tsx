import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen } from '@testing-library/react';
import { beforeEach, describe, expect, test, vi } from 'vitest';
import { ModelPicker } from './model-picker';

// 目录内容由用例控制：一条正常模型，外加「目录为空」那条分支要用的空数组。
const catalog = vi.hoisted(() => ({
  items: [{ slug: 'deepseek-chat', name: 'DeepSeek Chat', vendor: 'DeepSeek' }] as {
    slug: string;
    name: string;
    vendor?: string;
  }[],
}));

vi.mock('@/api', () => ({
  listModels: () => Promise.resolve({ status: 200, data: catalog }),
}));

function renderPicker(props: {
  value: string[];
  extra?: string[];
  onChange: (next: string[]) => void;
}) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <ModelPicker {...props} />
    </QueryClientProvider>,
  );
}

describe('ModelPicker', () => {
  beforeEach(() => {
    catalog.items = [{ slug: 'deepseek-chat', name: 'DeepSeek Chat', vendor: 'DeepSeek' }];
  });

  /**
   * 作品上带着目录外的 slug（已退役 / 已合并 / 还没核验）时，它必须仍然显示出来。
   *
   * 不显示的话，作者既看不到也删不掉；保存一次就被悄悄清掉，而他什么都没改。
   * 这条同时钉住「能取消」：取消后提交的声明里不再有它。
   */
  test('目录外的声明仍显示且可取消', async () => {
    const onChange = vi.fn();
    renderPicker({ value: ['deepseek-chat', 'retired-model'], extra: ['retired-model'], onChange });

    const orphan = await screen.findByLabelText('retired-model（不在可选目录中）');
    expect((orphan as HTMLInputElement).checked).toBe(true);
    expect(screen.getByLabelText('DeepSeek Chat（DeepSeek）')).toBeTruthy();

    fireEvent.click(orphan);
    expect(onChange).toHaveBeenCalledWith(['deepseek-chat']);
  });

  /**
   * 目录为空是合法的（主数据还没导入）：此时没有可选项，也不该给手填入口。
   *
   * 2026-11-06 曾有一版手填入口，2026-11-17 撤掉（#296 → #465）。这条按结构断言
   * 「没有文本框」，而不是按某个 aria 名——换个 label 就能绕过的断言不算判据。
   */
  test('目录为空时不给手填入口', async () => {
    catalog.items = [];
    const onChange = vi.fn();
    renderPicker({ value: [], onChange });

    expect(await screen.findByText('模型目录还没有内容，暂时无法声明模型。')).toBeTruthy();
    expect(screen.queryAllByRole('textbox')).toEqual([]);
    expect(screen.queryByRole('button', { name: '添加' })).toBeNull();
  });
});
