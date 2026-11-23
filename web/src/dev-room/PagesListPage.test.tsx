import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { expect, test } from 'vitest';
import { PagesListPage } from './PagesListPage';

test('页面清单列出 ready 与 fixture，并给出计数', () => {
  render(
    <MemoryRouter>
      <PagesListPage />
    </MemoryRouter>,
  );
  expect(screen.getByRole('heading', { level: 1, name: '页面清单' })).toBeInTheDocument();
  expect(
    screen.getByText(/45 条页面（ready \+ fixture \+ DEV 工具），按区列出。/),
  ).toBeInTheDocument();
  expect(screen.getByRole('link', { name: '首页' })).toHaveAttribute('href', '/');
  expect(screen.getByRole('link', { name: '第三方基准' })).toHaveAttribute('href', '/benchmarks');
});
