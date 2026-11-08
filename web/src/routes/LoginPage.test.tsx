import { fireEvent, render, screen } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router';
import { beforeEach, expect, test, vi } from 'vitest';
import { LoginPage } from './LoginPage';

// 登录页是「回执链路」的入口和出口：
// 进来时带着 return_to，出去时（注册 / 忘记密码 / 验证邮箱）还得把它带走，
// 否则用户从投稿页出发、登录完却落在首页。
// 另一头是安全：这个参数直接进 navigate，只放行站内路径。

const loginMock = vi.fn();
const setMeMock = vi.fn();

vi.mock('@/api', () => ({
  postAuthLogin: (...args: unknown[]) => loginMock(...args),
}));

vi.mock('@/features/auth/AuthContext', () => ({
  useAuth: () => ({
    me: null,
    isLoading: false,
    isError: false,
    setMe: setMeMock,
    signOut: vi.fn(),
  }),
}));

function renderPage(initialEntry: string) {
  return render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <Routes>
        <Route path="/login" element={<LoginPage />} />
        <Route path="/submit" element={<div>投稿页</div>} />
        <Route path="/register" element={<div>注册页</div>} />
        <Route path="/password-reset" element={<div>找回密码页</div>} />
        <Route path="/" element={<div>首页</div>} />
      </Routes>
    </MemoryRouter>,
  );
}

beforeEach(() => {
  loginMock.mockReset();
  setMeMock.mockReset();
});

test('落地页在注册与忘记密码链接上继续往下传', () => {
  renderPage('/login?return_to=%2Fsubmit');

  expect(screen.getByRole('link', { name: '注册' })).toHaveAttribute(
    'href',
    '/register?return_to=%2Fsubmit',
  );
  expect(screen.getByRole('link', { name: '忘记密码' })).toHaveAttribute(
    'href',
    '/password-reset?return_to=%2Fsubmit',
  );
});

test('页面上说清登录后会回到哪儿', () => {
  renderPage('/login?return_to=%2Fsubmit');
  expect(screen.getByRole('status')).toHaveTextContent('登录后会回到投稿页');
});

test('没有落脚点时不说废话', () => {
  renderPage('/login');
  expect(screen.queryByRole('status')).not.toBeInTheDocument();
  expect(screen.getByRole('link', { name: '注册' })).toHaveAttribute('href', '/register');
});

test.each([
  ['https://evil.example', '外站绝对地址'],
  ['//evil.example/x', '协议相对地址'],
])('拒绝 %s（%s）', (bad) => {
  renderPage(`/login?return_to=${encodeURIComponent(bad)}`);

  expect(screen.queryByRole('status')).not.toBeInTheDocument();
  expect(screen.getByRole('link', { name: '注册' })).toHaveAttribute('href', '/register');
});

test('登录成功后回到原来的页面', async () => {
  loginMock.mockResolvedValue({
    status: 200,
    data: { sub: 'u1', username: 'u', displayName: 'U' },
  });
  renderPage('/login?return_to=%2Fsubmit');

  fireEvent.change(screen.getByLabelText('邮箱或用户名'), { target: { value: 'u' } });
  fireEvent.change(screen.getByLabelText('密码'), { target: { value: 'secret-pass' } });
  fireEvent.click(screen.getByRole('button', { name: '登录' }));

  expect(await screen.findByText('投稿页')).toBeInTheDocument();
});

test('带着脏值登录也不会被送出站', async () => {
  loginMock.mockResolvedValue({
    status: 200,
    data: { sub: 'u1', username: 'u', displayName: 'U' },
  });
  renderPage('/login?return_to=%2F%2Fevil.example%2Fx');

  fireEvent.change(screen.getByLabelText('邮箱或用户名'), { target: { value: 'u' } });
  fireEvent.change(screen.getByLabelText('密码'), { target: { value: 'secret-pass' } });
  fireEvent.click(screen.getByRole('button', { name: '登录' }));

  // 回落到站内首页：不跳外站，也不停在登录页自己转圈。
  expect(await screen.findByText('首页')).toBeInTheDocument();
});

// 验证邮箱 / 找回密码处理完会把回执带到登录页。这两个参数过去只被写、没人读，
// 用户走完一整条验证流程，最后看到的是一张跟刚打开时一模一样的表单。
test('验证邮箱回来时说明上一步成了', () => {
  renderPage('/login?verified=1');
  expect(screen.getByRole('status')).toHaveTextContent('邮箱验证完成');
});

test('重设密码回来时说明旧会话已退出', () => {
  renderPage('/login?reset=1');
  const notice = screen.getByRole('status');
  expect(notice).toHaveTextContent('密码已经重设');
  expect(notice).toHaveTextContent('此前的登录设备都已退出');
});
