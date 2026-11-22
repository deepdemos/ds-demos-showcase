import { render, screen } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router';
import { describe, expect, test } from 'vitest';
import { AUTH } from './auth-layout';

describe('auth-layout', () => {
  test('认证页不套主导航，保留品牌与回作品库', () => {
    render(
      <MemoryRouter initialEntries={['/login']}>
        <Routes>
          <Route element={<AUTH />}>
            <Route path="/login" element={<div>登录表单</div>} />
          </Route>
        </Routes>
      </MemoryRouter>,
    );
    expect(screen.queryByRole('navigation', { name: '主导航' })).toBeNull();
    expect(screen.getByRole('link', { name: 'DeepDemos 展示' })).toHaveAttribute('href', '/');
    expect(screen.getByRole('link', { name: '回作品库' })).toHaveAttribute('href', '/demos');
    expect(screen.getByText('登录表单')).toBeInTheDocument();
  });
});
