import { fireEvent, render, screen } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router';
import { describe, expect, test, vi } from 'vitest';
import { SubmitPage } from './SubmitPage';

const auth = vi.hoisted(() => ({
  me: null as null | { sub: string; username: string; displayName: string },
  isLoading: false,
}));

vi.mock('@/features/auth/AuthContext', () => ({
  useAuth: () => ({ ...auth, isError: false, setMe: vi.fn(), signOut: vi.fn() }),
}));

function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path="/submit" element={<SubmitPage />} />
        <Route path="/login" element={<div>登录页</div>} />
      </Routes>
    </MemoryRouter>,
  );
}

describe('SubmitPage', () => {
  test('未登录时跳登录页并带上 return_to', () => {
    auth.me = null;
    auth.isLoading = false;
    renderAt('/submit');
    expect(screen.getByText('登录页')).toBeInTheDocument();
  });

  test('没有选文件时提交给出提示，不发请求', () => {
    auth.me = { sub: 'x', username: 'u', displayName: 'U' };
    auth.isLoading = false;
    renderAt('/submit');

    fireEvent.change(screen.getByLabelText('标题'), { target: { value: '我的作品' } });
    fireEvent.click(screen.getByRole('button', { name: '提交投稿' }));

    expect(screen.getByText('请选择要上传的作品文件')).toBeInTheDocument();
  });
});
