import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { describe, expect, it } from 'vitest';

import { BenchmarkSourcesPage } from './BenchmarkSourcesPage';
import { benchmarkSources } from '@/content/benchmark-sources';

function renderPage() {
  return render(
    <MemoryRouter>
      <BenchmarkSourcesPage />
    </MemoryRouter>,
  );
}

describe('BenchmarkSourcesPage', () => {
  it('按象限分组列出上游榜单', () => {
    renderPage();

    expect(screen.getByRole('heading', { name: /编码与软件工程/ })).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: /工具与 agent/ })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Bug Hunt Bench' })).toHaveAttribute(
      'href',
      'https://bughunt.productcompass.pm',
    );
  });

  it('外链一律新窗口打开且带安全属性', () => {
    renderPage();

    const link = screen.getByRole('link', { name: 'Bug Hunt Bench' });
    expect(link).toHaveAttribute('target', '_blank');
    expect(link).toHaveAttribute('rel', 'noreferrer noopener');
  });

  it('档位如实标注，不把「仅引用」写成缺数据', () => {
    renderPage();

    expect(screen.getAllByText('有分数').length).toBeGreaterThan(0);
    expect(screen.getAllByText('仅引用').length).toBeGreaterThan(0);
  });

  it('成本缺失写「未公布」，不补零', () => {
    renderPage();

    expect(screen.getAllByText(/未公布/).length).toBeGreaterThan(0);
  });

  it('不把内部证据标记带上对外页', () => {
    renderPage();

    expect(screen.queryByText(/\[A\]/)).toBeNull();
    expect(screen.queryByText(/未核/)).toBeNull();
  });

  it('表头带 scope，读屏能把列头对上单元格', () => {
    renderPage();

    const headers = screen.getAllByRole('columnheader');
    expect(headers.length).toBeGreaterThan(0);
    for (const header of headers) {
      expect(header).toHaveAttribute('scope', 'col');
    }
  });

  it('每一行的档位都在「档位」说明里，不出现空白徽标', () => {
    const legend = benchmarkSources.tierLegend.map((tier) => tier.value);

    for (const source of benchmarkSources.sources) {
      expect(legend, source.id).toContain(source.displayTier);
    }
  });

  it('内容文件里的每一行都能在页面上找到', () => {
    renderPage();

    for (const source of benchmarkSources.sources) {
      expect(screen.getByRole('link', { name: source.name })).toBeInTheDocument();
    }
  });
});
