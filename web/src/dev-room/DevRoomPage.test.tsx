import { fireEvent, render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { afterEach, expect, test } from 'vitest';
import { DevRoomPage } from './DevRoomPage';

afterEach(() => {
  localStorage.clear();
  document.documentElement.classList.remove('ring-ui-theme-dark');
});

test('支持分类筛选、分页与主题切换', () => {
  render(
    <MemoryRouter initialEntries={['/dev-room']}>
      <DevRoomPage />
    </MemoryRouter>,
  );

  expect(document.querySelectorAll('[data-control-id]')).toHaveLength(6);
  expect(screen.getByText('第 1 / 2 页')).toBeInTheDocument();

  fireEvent.click(screen.getByRole('button', { name: '下一页' }));
  expect(screen.getByText('第 2 / 2 页')).toBeInTheDocument();
  expect(document.querySelectorAll('[data-control-id]')).toHaveLength(4);

  fireEvent.click(screen.getByRole('button', { name: '输入' }));
  expect(screen.getByText('共 3 个控件')).toBeInTheDocument();
  expect(document.querySelectorAll('[data-control-id]')).toHaveLength(3);

  fireEvent.click(screen.getByRole('button', { name: '切换到深色模式' }));
  expect(document.documentElement).toHaveClass('ring-ui-theme-dark');

  fireEvent.click(screen.getByRole('button', { name: '切换到浅色模式' }));
  expect(document.documentElement).not.toHaveClass('ring-ui-theme-dark');
});
