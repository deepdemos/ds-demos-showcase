import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { expect, test } from 'vitest';
import { SettingsPage } from './SettingsPage';

test('设置页给出 DEV 资料入口与内容管理入口', () => {
  render(
    <MemoryRouter>
      <SettingsPage />
    </MemoryRouter>,
  );
  expect(screen.getByRole('heading', { level: 1, name: '设置' })).toBeInTheDocument();
  expect(screen.getByRole('link', { name: '资料设置' })).toHaveAttribute(
    'href',
    '/me/settings/profile',
  );
  expect(screen.getByRole('link', { name: '收藏夹' })).toHaveAttribute('href', '/me/collections');
});
