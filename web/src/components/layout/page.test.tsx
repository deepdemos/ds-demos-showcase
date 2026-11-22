import { render, screen } from '@testing-library/react';
import { describe, expect, test } from 'vitest';
import { Page, PageSection } from './page';

describe('Page', () => {
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
