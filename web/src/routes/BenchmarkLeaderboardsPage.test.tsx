import { fireEvent, render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { describe, expect, it } from 'vitest';

import { BenchmarkLeaderboardsPage } from './BenchmarkLeaderboardsPage';

function renderPage() {
  return render(
    <MemoryRouter>
      <BenchmarkLeaderboardsPage />
    </MemoryRouter>,
  );
}

describe('BenchmarkLeaderboardsPage', () => {
  it('按能力分组列出榜卡片', () => {
    renderPage();

    expect(screen.getByRole('heading', { name: /编码与软件工程/ })).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: /工具与 agent/ })).toBeInTheDocument();
    expect(screen.getByText('SWE-bench Verified')).toBeInTheDocument();
  });

  it('卡片外链新窗口打开且带安全属性', () => {
    renderPage();

    const link = screen.getByRole('link', { name: /SWE-bench Verified/ });
    expect(link).toHaveAttribute('target', '_blank');
    expect(link).toHaveAttribute('rel', 'noreferrer noopener');
  });

  it('默认只显示近期榜，切「全部」后旧榜出现', () => {
    renderPage();

    expect(screen.queryByText('MMLU')).toBeNull();

    fireEvent.change(screen.getByLabelText('新鲜度'), { target: { value: 'all' } });

    expect(screen.getByText('MMLU')).toBeInTheDocument();
  });

  it('搜索只保留匹配的榜', () => {
    renderPage();

    fireEvent.change(screen.getByLabelText('搜榜名或描述'), {
      target: { value: 'GPQA' },
    });

    expect(screen.getByText('GPQA Diamond')).toBeInTheDocument();
    expect(screen.queryByText('SWE-bench Verified')).toBeNull();
  });

  it('每张卡片都带模型数与最新记录，不出现空卡片', () => {
    renderPage();

    for (const name of ['SWE-bench Verified', 'GPQA Diamond']) {
      const card = screen.getByText(name).closest('a');
      expect(card, name).not.toBeNull();
      expect(card?.textContent, name).toMatch(/\d+ 个模型/);
    }
  });
});
