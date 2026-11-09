import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { describe, expect, test } from 'vitest';
import { NotFoundPage } from './NotFoundPage';

function renderPage() {
  return render(
    <MemoryRouter>
      <NotFoundPage />
    </MemoryRouter>,
  );
}

describe('NotFoundPage', () => {
  // 事故现场：用户打开 /nope，页面上只有「404」和「页面不存在。」，没有任何可点的去处。
  test('给出回作品库与回首页两条出路', () => {
    renderPage();

    expect(screen.getByRole('link', { name: '回作品库' })).toHaveAttribute('href', '/demos');
    expect(screen.getByRole('link', { name: '回首页' })).toHaveAttribute('href', '/');
  });

  test('说明为什么可能看不到这一页', () => {
    renderPage();

    expect(screen.getByRole('heading', { name: '404' })).toBeInTheDocument();
    expect(screen.getByText(/地址写错了/)).toBeInTheDocument();
  });
});
