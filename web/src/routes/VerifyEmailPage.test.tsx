import { fireEvent, render, screen } from '@testing-library/react';
import { MemoryRouter, Route, Routes, useSearchParams } from 'react-router';
import { beforeEach, expect, test, vi } from 'vitest';
import { VerifyEmailPage } from './VerifyEmailPage';

// 验证页有两个「不能错」的地方：验证失败不能假装成功（会让人以为验证过了，
// 然后登录又被拒），重发成功不能说成「信已发出」（不存在的邮箱也回 204）。
// 这两条各钉一个用例。

const verifyMock = vi.fn();
const resendMock = vi.fn();

vi.mock('@/api', () => ({
  postAuthVerifyEmail: (...args: unknown[]) => verifyMock(...args),
  postAuthVerifyEmailResend: (...args: unknown[]) => resendMock(...args),
}));

function renderPage(initialEntry = '/verify-email?email=a%40b.co') {
  return render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <Routes>
        <Route path="/verify-email" element={<VerifyEmailPage />} />
        <Route path="/login" element={<div>登录页</div>} />
      </Routes>
    </MemoryRouter>,
  );
}

beforeEach(() => {
  verifyMock.mockReset();
  resendMock.mockReset();
});

test('验证通过后跳登录页（不自动登录）', async () => {
  verifyMock.mockResolvedValue({ status: 204, data: undefined });
  renderPage();

  fireEvent.change(screen.getByLabelText('验证码'), { target: { value: '123456' } });
  fireEvent.click(screen.getByRole('button', { name: '完成验证' }));

  expect(await screen.findByText('登录页')).toBeInTheDocument();
  expect(verifyMock).toHaveBeenCalledWith({ email: 'a@b.co', code: '123456' });
});

test('验证码不对时留在页面并显示原因', async () => {
  verifyMock.mockResolvedValue({
    status: 401,
    data: { code: 'unauthorized', message: '验证码不对或已过期' },
  });
  renderPage();

  fireEvent.change(screen.getByLabelText('验证码'), { target: { value: '000000' } });
  fireEvent.click(screen.getByRole('button', { name: '完成验证' }));

  expect(await screen.findByRole('alert')).toHaveTextContent('验证码不对或已过期');
  expect(screen.queryByText('登录页')).not.toBeInTheDocument();
});

test('注册时信没发出去，页面上要明说', () => {
  renderPage('/verify-email?email=a%40b.co&sent=0');
  expect(screen.getByRole('status')).toHaveTextContent('验证信没发出去');
});

test('重发只说「已受理」，不承诺信真的到了', async () => {
  resendMock.mockResolvedValue({ status: 204, data: undefined });
  renderPage();

  fireEvent.click(screen.getByRole('button', { name: '重新发送验证码' }));

  const notice = await screen.findByRole('status');
  expect(notice).toHaveTextContent('如果这个邮箱还没验证过');
  // 没有「已发送到你的邮箱」这种承诺：不存在的邮箱也回 204。
  expect(notice).not.toHaveTextContent('已发送到');
});

// 回执链路：验证页是「注册 → 验证 → 登录 → 原页面」这条路上的一站，
// 它如果把 return_to 丢了，前面三处拼得再对也白搭。
function LoginProbe() {
  const [params] = useSearchParams();
  return (
    <div data-testid="login-probe">
      verified={params.get('verified')} return_to={params.get('return_to')}
    </div>
  );
}

test('验证成功后把落脚点带回登录页', async () => {
  verifyMock.mockResolvedValue({ status: 204, data: undefined });
  render(
    <MemoryRouter initialEntries={['/verify-email?email=a%40b.co&return_to=%2Fsubmit']}>
      <Routes>
        <Route path="/verify-email" element={<VerifyEmailPage />} />
        <Route path="/login" element={<LoginProbe />} />
      </Routes>
    </MemoryRouter>,
  );

  fireEvent.change(screen.getByLabelText('验证码'), { target: { value: '123456' } });
  fireEvent.click(screen.getByRole('button', { name: '完成验证' }));

  expect(await screen.findByTestId('login-probe')).toHaveTextContent(
    'verified=1 return_to=/submit',
  );
});

test('页脚的「去登录」也带着落脚点', () => {
  renderPage('/verify-email?email=a%40b.co&return_to=%2Fsubmit');
  expect(screen.getByRole('link', { name: '去登录' })).toHaveAttribute(
    'href',
    '/login?return_to=%2Fsubmit',
  );
});
