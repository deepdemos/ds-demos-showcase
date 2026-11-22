import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { expect, test } from 'vitest';
import { AboutPage } from './AboutPage';

test('关于页说明作品来源与数据口径', () => {
  render(
    <MemoryRouter>
      <AboutPage />
    </MemoryRouter>,
  );
  expect(screen.getByRole('heading', { level: 1, name: '关于' })).toBeInTheDocument();
  expect(screen.getByText('数据与排序')).toBeInTheDocument();
  expect(screen.getByRole('link', { name: '模型目录' })).toHaveAttribute('href', '/models');
});
