import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { beforeEach, expect, test, vi } from 'vitest';
import { PasswordResetPage } from './PasswordResetPage';

/**
 * 找回密码页的三条要害：
 *
 * 1. 第一步只回「已受理」，不能说出这个邮箱存不存在——
 *    后端刻意让不存在的邮箱也回 204，页面把它说漏就等于把防枚举做空了。
 * 2. 两次密码不一致时不发请求，本地就挡住。
 * 3. 重设失败时转述后端原话（后端把「码错了 / 过期 / 超次数 / 邮箱不存在」合并成 401）。
 */

const resetCode = vi.fn();
const reset = vi.fn();

vi.mock('@/api', () => ({
  postAuthPasswordResetCode: (...args: unknown[]) => resetCode(...args),
  postAuthPasswordReset: (...args: unknown[]) => reset(...args),
}));

function renderPage() {
  return render(
    <MemoryRouter>
      <PasswordResetPage />
    </MemoryRouter>,
  );
}

function fill(label: RegExp, value: string) {
  fireEvent.change(screen.getByLabelText(label), { target: { value } });
}

beforeEach(() => {
  resetCode.mockReset();
  reset.mockReset();
});

test('申请验证码后只说「已受理」，不承诺邮箱存在', async () => {
  resetCode.mockResolvedValueOnce({ status: 204, data: undefined });
  renderPage();

  fill(/邮箱/, 'someone@example.com');
  screen.getByRole('button', { name: '获取验证码' }).click();

  await waitFor(() => {
    expect(screen.getByRole('status').textContent).toContain('如果这个邮箱注册过');
  });
  expect(screen.getByRole('status').textContent).not.toContain('已发送到你的邮箱');
});

test('两次密码不一致时本地就挡住，不发请求', async () => {
  resetCode.mockResolvedValueOnce({ status: 204, data: undefined });
  renderPage();

  fill(/邮箱/, 'someone@example.com');
  screen.getByRole('button', { name: '获取验证码' }).click();
  await waitFor(() => expect(screen.getByLabelText(/验证码/)).toBeTruthy());

  // 原生 `required` 会先挡住空验证码，所以这里要先把它填上，
  // 否则测到的是「浏览器不许提交」，而不是「两次密码不一致」。
  fill(/验证码/, '123456');
  fill(/^新密码/, 'password-one');
  fill(/再输一次/, 'password-two');
  screen.getByRole('button', { name: '重设密码' }).click();

  await waitFor(() => {
    expect(screen.getByRole('alert').textContent).toContain('两次输入的密码不一致');
  });
  expect(reset).not.toHaveBeenCalled();
});

test('重设被拒时显示后端原话', async () => {
  resetCode.mockResolvedValueOnce({ status: 204, data: undefined });
  reset.mockResolvedValueOnce({
    status: 401,
    data: { code: 'unauthorized', message: '验证码不对或已过期' },
  });
  renderPage();

  fill(/邮箱/, 'someone@example.com');
  screen.getByRole('button', { name: '获取验证码' }).click();
  await waitFor(() => expect(screen.getByLabelText(/验证码/)).toBeTruthy());

  fill(/验证码/, '123456');
  fill(/^新密码/, 'password-one');
  fill(/再输一次/, 'password-one');
  screen.getByRole('button', { name: '重设密码' }).click();

  await waitFor(() => {
    expect(screen.getByRole('alert').textContent).toContain('验证码不对或已过期');
  });
});
