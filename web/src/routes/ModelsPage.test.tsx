import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { expect, test, vi } from 'vitest';
import { listModels } from '@/api';
import { ModelsPage } from './ModelsPage';

vi.mock('@/api', () => ({ listModels: vi.fn() }));

function model(name: string, slug: string, vendor: string) {
  return { name, slug, vendor, status: 'active' } as never;
}

test('模型目录列出模型并可搜索', async () => {
  vi.mocked(listModels).mockResolvedValue({
    status: 200,
    data: {
      items: [
        model('DeepSeek Chat', 'deepseek-chat', 'DeepSeek'),
        model('Claude', 'claude', 'Anthropic'),
      ],
    },
  } as never);

  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={['/models']}>
        <ModelsPage />
      </MemoryRouter>
    </QueryClientProvider>,
  );

  expect(await screen.findByRole('link', { name: 'DeepSeek Chat' })).toHaveAttribute(
    'href',
    '/models/deepseek-chat',
  );
  fireEvent.change(screen.getByLabelText('搜索模型'), { target: { value: 'claude' } });
  fireEvent.click(screen.getByRole('button', { name: '搜索' }));
  expect(await screen.findByRole('link', { name: 'Claude' })).toBeInTheDocument();
  expect(screen.queryByRole('link', { name: 'DeepSeek Chat' })).toBeNull();
});

test('厂商为空时显示「厂商未标注」，不是「厂商未知」', async () => {
  vi.mocked(listModels).mockResolvedValue({
    status: 200,
    data: { items: [model('私有微调', 'private-finetune', '')] },
  } as never);

  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={['/models']}>
        <ModelsPage />
      </MemoryRouter>
    </QueryClientProvider>,
  );

  // 空厂商是**未标注**（没人填），不是**未知**（不知道用了什么）——两种话在界面上
  // 会引导出不同的动作，模型页与列表必须说同一句。
  expect(await screen.findByText(/厂商未标注/)).toBeInTheDocument();
  expect(screen.queryByText(/厂商未知/)).toBeNull();
});
