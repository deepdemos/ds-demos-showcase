import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { expect, test } from 'vitest';
import { UnavailablePage } from './UnavailablePage';

test('生产 fixture 兜底页说明尚未接入并给出出口', () => {
  render(
    <MemoryRouter>
      <UnavailablePage />
    </MemoryRouter>,
  );
  expect(screen.getByRole('heading', { level: 1, name: '功能尚未接入' })).toBeInTheDocument();
  expect(screen.getByRole('link', { name: '回作品库' })).toHaveAttribute('href', '/demos');
  expect(screen.getByRole('link', { name: '回首页' })).toHaveAttribute('href', '/');
});
