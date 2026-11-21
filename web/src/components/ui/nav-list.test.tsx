import { fireEvent, render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { describe, expect, test, vi } from 'vitest';
import { NavList } from './nav-list';

describe('NavList', () => {
  test('链接项用 aria-current 标当前页', () => {
    render(
      <MemoryRouter initialEntries={['/demos']}>
        <NavList
          ariaLabel="导航"
          items={[
            { id: 'demos', label: '作品库', to: '/demos' },
            { id: 'rankings', label: '排行榜', to: '/rankings' },
          ]}
        />
      </MemoryRouter>,
    );
    expect(screen.getByRole('link', { name: '作品库' })).toHaveAttribute('aria-current', 'page');
    expect(screen.getByRole('link', { name: '排行榜' })).not.toHaveAttribute('aria-current');
  });

  test('按钮项用 activeId 标当前页并可点击', () => {
    const onSelect = vi.fn();
    render(
      <NavList
        ariaLabel="导航"
        activeId="b"
        orientation="vertical"
        items={[
          { id: 'a', label: '甲', onSelect },
          { id: 'b', label: '乙', onSelect },
        ]}
      />,
    );
    expect(screen.getByRole('button', { name: '乙' })).toHaveAttribute('aria-current', 'page');
    fireEvent.click(screen.getByRole('button', { name: '甲' }));
    expect(onSelect).toHaveBeenCalled();
  });
});
