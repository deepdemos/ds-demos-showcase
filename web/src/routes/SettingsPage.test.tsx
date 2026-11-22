import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { expect, test } from 'vitest';
import { SettingsPage } from './SettingsPage';

test('设置页说明资料尚未接入并给出内容管理入口', () => {
  render(
    <MemoryRouter>
      <SettingsPage />
    </MemoryRouter>,
  );
  expect(screen.getByRole('heading', { level: 1, name: '设置' })).toBeInTheDocument();
  expect(screen.getByText(/资料与偏好设置尚未接入/)).toBeInTheDocument();
  expect(screen.getByRole('link', { name: '收藏夹' })).toHaveAttribute('href', '/me/collections');
});
