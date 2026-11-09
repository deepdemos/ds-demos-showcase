import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { describe, expect, test, vi } from 'vitest';
import { HomePage } from './HomePage';

const auth = vi.hoisted(() => ({
  me: null as null | { sub: string; username: string; displayName: string },
}));

vi.mock('@/features/auth/AuthContext', () => ({
  useAuth: () => ({
    me: auth.me,
    isLoading: false,
    isError: false,
    setMe: vi.fn(),
    signOut: vi.fn(),
  }),
}));

function renderHome() {
  return render(
    <MemoryRouter>
      <HomePage />
    </MemoryRouter>,
  );
}

describe('HomePage', () => {
  test('未登录：给出作品库、排行榜和登录三个入口', () => {
    auth.me = null;
    renderHome();

    expect(screen.getByRole('link', { name: /浏览作品库/ })).toHaveAttribute('href', '/demos');
    expect(screen.getByRole('link', { name: '排行榜' })).toHaveAttribute('href', '/rankings');
    expect(screen.getByRole('link', { name: '登录' })).toHaveAttribute('href', '/login');
    expect(screen.queryByRole('link', { name: '投稿' })).toBeNull();
  });

  test('已登录：把登录换成投稿', () => {
    auth.me = { sub: 'u1', username: 'alice', displayName: 'Alice' };
    renderHome();

    expect(screen.getByRole('link', { name: '投稿' })).toHaveAttribute('href', '/submit');
    expect(screen.queryByRole('link', { name: '登录' })).toBeNull();
  });

  // 首页曾经写着「目前开放的是作品库（只读）」——那是在投稿、评分、审核、排行榜都上线之前的事实。
  // 这条断言钉住「首页不再宣称自己是只读的」。
  test('不再宣称站点是只读的', () => {
    auth.me = null;
    const { container } = renderHome();

    expect(container.textContent).not.toContain('只读');
  });
});
