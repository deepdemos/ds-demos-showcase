import { render, screen } from '@testing-library/react';
import { describe, expect, test } from 'vitest';
import { Page, PageSection, PageWidthProvider } from './page';

describe('Page', () => {
  test('路由提供的 width 覆盖页面自己的 width', () => {
    render(
      <PageWidthProvider width="narrow">
        <Page width="wide" title="浏览历史">
          内容
        </Page>
      </PageWidthProvider>,
    );
    const page = screen.getByTestId('page');
    expect(page).toHaveClass('max-w-3xl');
    expect(page).not.toHaveClass('max-w-6xl');
  });

  test('渲染抬头、说明与操作', () => {
    render(
      <Page title="作品库" description="浏览公开作品" actions={<button type="button">投稿</button>}>
        <PageSection title="最近上架">内容</PageSection>
      </Page>,
    );
    expect(screen.getByRole('heading', { level: 1, name: '作品库' })).toBeInTheDocument();
    expect(screen.getByText('浏览公开作品')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '投稿' })).toBeInTheDocument();
    expect(screen.getByRole('heading', { level: 2, name: '最近上架' })).toBeInTheDocument();
  });
});
