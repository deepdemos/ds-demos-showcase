import { render, screen, within } from '@testing-library/react';
import { MemoryRouter, Route, Routes, useSearchParams } from 'react-router';
import { beforeEach, describe, expect, test, vi } from 'vitest';
import type { Me } from '@/api';
import { AdminLayout } from './AdminLayout';

const auth = vi.hoisted(() => ({
  me: null as Me | null,
  isLoading: false,
}));

vi.mock('@/features/auth/AuthContext', () => ({
  useAuth: () => ({ ...auth, setMe: vi.fn(), isError: false, signOut: vi.fn() }),
}));

function user(permissions: string[] = [], role = 'user'): Me {
  return {
    sub: 'u1',
    username: 'alice',
    displayName: 'Alice',
    email: 'a@example.com',
    role,
    status: 'active',
    level: 0,
    createdAt: '2026-01-01T00:00:00+08:00',
    permissions,
  } as Me;
}

function LoginProbe() {
  const [params] = useSearchParams();
  return <p>登录页 {params.get('return_to') ?? '无来路'}</p>;
}

function renderAt(path = '/admin/inbox') {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route element={<AdminLayout />}>
          <Route path="/admin/inbox" element={<div>收件箱正文</div>} />
          <Route path="/admin/reviews" element={<div>审核正文</div>} />
          <Route path="/admin/models" element={<div>模型正文</div>} />
        </Route>
        <Route path="/login" element={<LoginProbe />} />
      </Routes>
    </MemoryRouter>,
  );
}

beforeEach(() => {
  auth.me = user(['demo.review']);
  auth.isLoading = false;
});

describe('AdminLayout', () => {
  test('按权限渲染分组导航与子页面', () => {
    renderAt();
    const nav = screen.getByRole('navigation', { name: '管理导航 - 待办' });
    expect(within(nav).getByRole('link', { name: '收件箱' })).toHaveAttribute(
      'href',
      '/admin/inbox',
    );
    expect(within(nav).getByRole('link', { name: '作品审核' })).toHaveAttribute(
      'href',
      '/admin/reviews',
    );
    expect(screen.queryByRole('link', { name: '模型导入' })).toBeNull();
    expect(screen.getByText('收件箱正文')).toBeInTheDocument();
  });

  test('admin 角色能看到内容分组', () => {
    auth.me = user([], 'admin');
    renderAt();
    const content = screen.getByRole('navigation', { name: '管理导航 - 内容' });
    expect(within(content).getByRole('link', { name: '模型导入' })).toHaveAttribute(
      'href',
      '/admin/models',
    );
  });

  test('没有管理权限时显示 403，而不是空白', () => {
    auth.me = user();
    renderAt();
    expect(screen.getByRole('heading', { level: 1, name: '403' })).toBeInTheDocument();
    expect(screen.queryByRole('navigation', { name: '管理导航（移动端）' })).toBeNull();
  });

  test('角色是 moderator 但没有可显示的入口时也显示 403', () => {
    auth.me = user([], 'moderator');
    renderAt();
    expect(screen.getByRole('heading', { level: 1, name: '403' })).toBeInTheDocument();
  });

  test('未登录时带当前路径跳登录页', async () => {
    auth.me = null;
    renderAt('/admin/models?q=x');
    expect(await screen.findByText('登录页 /admin/models?q=x')).toBeInTheDocument();
  });
});
