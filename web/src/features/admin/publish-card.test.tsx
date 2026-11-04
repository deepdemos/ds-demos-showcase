import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router';
import { beforeEach, describe, expect, test, vi } from 'vitest';
import { PublishCard } from './publish-card';

const publishDemo = vi.hoisted(() => vi.fn());

vi.mock('@/api', () => ({ publishDemo }));

function renderCard() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={['/admin/reviews']}>
        <Routes>
          <Route path="/admin/reviews" element={<PublishCard slug="demo-abc" title="一个作品" />} />
          <Route path="/demos/:slug" element={<div>作品页</div>} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  publishDemo.mockReset();
});

describe('PublishCard', () => {
  test('默认按 deep / listed / zh 发布，成功后换成结果态', async () => {
    publishDemo.mockResolvedValue({ status: 200, data: {} });
    renderCard();

    expect(screen.getByLabelText('渠道')).toHaveValue('deep');
    expect(screen.getByLabelText('可见性')).toHaveValue('listed');
    expect(screen.getByLabelText('语言')).toHaveValue('zh');

    fireEvent.click(screen.getByRole('button', { name: '发布' }));

    await waitFor(() =>
      expect(publishDemo).toHaveBeenCalledWith('demo-abc', {
        channel: 'deep',
        visibility: 'listed',
        lang: 'zh',
      }),
    );
    // 发布是这条作品在这个界面上的终点，必须换成结果态并给出公开链接，
    // 否则管理员会怀疑自己到底点没点成功。
    expect(await screen.findByText(/已发布到「主站」/)).toBeInTheDocument();
    expect(screen.getByRole('link', { name: '查看作品页' })).toHaveAttribute(
      'href',
      '/demos/demo-abc',
    );
  });

  test('选 astra 会把语言锁成 en——渠道规则不是用户的选择', async () => {
    renderCard();

    fireEvent.change(screen.getByLabelText('渠道'), { target: { value: 'astra' } });

    expect(screen.getByLabelText('语言')).toHaveValue('en');
    expect(screen.getByLabelText('语言')).toBeDisabled();
    expect(screen.getByText(/Astra 是英文橱窗/)).toBeInTheDocument();
  });

  test('非 2xx 要报错并带上后端 message——orval 非 2xx 不 throw', async () => {
    // 409 = 这条作品此刻不在可发布状态（例如刚被退回审核）。
    publishDemo.mockResolvedValue({ status: 409, data: { message: '作品不处于可发布状态' } });
    renderCard();

    fireEvent.click(screen.getByRole('button', { name: '发布' }));

    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent('作品不处于可发布状态');
    });
    // 失败之后不能显示成功态。
    expect(screen.queryByText(/已发布到/)).not.toBeInTheDocument();
  });
});
