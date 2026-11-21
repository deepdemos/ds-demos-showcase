import { fireEvent, render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { describe, expect, test, vi } from 'vitest';
import { Menu } from './menu';

function renderMenu(onSelect = vi.fn()) {
  return render(
    <MemoryRouter>
      <Menu
        ariaLabel="测试菜单"
        label="Alice"
        items={[
          { id: 'a', label: '资料', onSelect: () => onSelect('资料') },
          { id: 'b', label: '登出', danger: true, onSelect: () => onSelect('登出') },
        ]}
      />
    </MemoryRouter>,
  );
}

describe('Menu', () => {
  test('点击触发按钮打开菜单，选择后关闭', () => {
    const onSelect = vi.fn();
    renderMenu(onSelect);
    fireEvent.click(screen.getByRole('button', { name: 'Alice' }));
    expect(screen.getByRole('menu', { name: '测试菜单' })).toBeInTheDocument();
    fireEvent.click(screen.getByRole('menuitem', { name: '资料' }));
    expect(onSelect).toHaveBeenCalledWith('资料');
    expect(screen.queryByRole('menu')).toBeNull();
  });

  test('Escape 关闭菜单并把焦点还给触发按钮', () => {
    renderMenu();
    const trigger = screen.getByRole('button', { name: 'Alice' });
    fireEvent.click(trigger);
    fireEvent.keyDown(document, { key: 'Escape' });
    expect(screen.queryByRole('menu')).toBeNull();
    expect(trigger).toHaveFocus();
  });
});
