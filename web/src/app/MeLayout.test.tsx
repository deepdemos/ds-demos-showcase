import { render, screen, within } from '@testing-library/react';
import { MemoryRouter, Route, Routes, useSearchParams } from 'react-router';
import { beforeEach, describe, expect, test, vi } from 'vitest';
import type { Me } from '@/api';
import { MeLayout } from './MeLayout';

const auth = vi.hoisted(() => ({
  me: null as Me | null,
  isLoading: false,
}));

vi.mock('@/features/auth/AuthContext', () => ({
  useAuth: () => ({ ...auth, setMe: vi.fn(), isError: false, signOut: vi.fn() }),
}));

function user(): Me {
  return {
    sub: 'u1',
    username: 'alice',
    displayName: 'Alice',
    email: 'a@example.com',
    role: 'user',
    status: 'active',
    level: 0,
    createdAt: '2026-01-01T00:00:00+08:00',
    permissions: [],
  } as Me;
}

function LoginProbe() {
  const [params] = useSearchParams();
  return <p>登录页 {params.get('return_to') ?? '无来路'}</p>;
}

function renderAt(path = '/me/demos') {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route element={<MeLayout />}>
          <Route path="/me/demos" element={<div>我的作品正文</div>} />
          <Route path="/me/history" element={<div>本机历史正文</div>} />
        </Route>
        <Route path="/login" element={<LoginProbe />} />
      </Routes>
    </MemoryRouter>,
  );
}

beforeEach(() => {
  auth.me = user();
  auth.isLoading = false;
});

describe('MeLayout', () => {
  test('未登录也能打开本机浏览历史（HIST-001）', () => {
    auth.me = null;
    renderAt('/me/history');
    expect(screen.getByText('本机历史正文')).toBeInTheDocument();
    expect(screen.queryByRole('navigation', { name: '个人中心导航' })).toBeNull();
  });

  test('登录后显示个人区导航与子页面', () => {
    renderAt();
    const nav = screen.getByRole('navigation', { name: '个人中心导航' });
    expect(within(nav).getByRole('link', { name: '我的作品' })).toHaveAttribute(
      'href',
      '/me/demos',
    );
    expect(within(nav).getByRole('link', { name: '收藏夹' })).toHaveAttribute(
      'href',
      '/me/collections',
    );
    expect(within(nav).getByRole('link', { name: '浏览历史' })).toHaveAttribute(
      'href',
      '/me/history',
    );
    expect(within(nav).getByRole('link', { name: '通知' })).toHaveAttribute(
      'href',
      '/me/notifications',
    );
    expect(within(nav).getByRole('link', { name: '设置' })).toHaveAttribute('href', '/me/settings');
    expect(screen.getByText('我的作品正文')).toBeInTheDocument();
  });

  test('未登录时带查询串跳登录页', async () => {
    auth.me = null;
    renderAt('/me/demos?state=deleted');
    expect(await screen.findByText('登录页 /me/demos?state=deleted')).toBeInTheDocument();
    expect(screen.queryByRole('navigation', { name: '个人中心导航' })).toBeNull();
  });
});
