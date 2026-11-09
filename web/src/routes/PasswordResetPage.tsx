import { useState, type FormEvent } from 'react';
import { Link, useNavigate, useSearchParams } from 'react-router';
import { Button, Input } from '@/components/ui';
import { postAuthPasswordReset, postAuthPasswordResetCode, type Error as ApiError } from '@/api';
import { PASSWORD_MIN, passwordLengthError } from '@/lib/password';
import { returnToParam } from '@/lib/return-to';

/**
 * 找回密码。
 *
 * 两步：先申请验证码，再用「验证码 + 新密码」重设。两步分开是接口的形状决定的——
 * 申请那一步后端明确说「无论邮箱注册没注册都回 204」，所以界面上不能出现
 * 「这个邮箱不存在」这类提示，否则等于把第一步的防枚举承诺在页面上说漏了。
 *
 * 重设成功后，后端会在同一个事务里吊销该账号此前签发的所有会话。
 * 所以这里不能「顺便登录」，而是把人送回登录页——他手上那些旧标签页已经全失效了。
 */
export function PasswordResetPage() {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const rawReturnTo = searchParams.get('return_to');

  const [email, setEmail] = useState('');
  const [code, setCode] = useState('');
  const [password, setPassword] = useState('');
  const [confirm, setConfirm] = useState('');

  const [codeSent, setCodeSent] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);

  const requestCode = async () => {
    setBusy(true);
    setError(null);
    setNotice(null);
    try {
      const response = await postAuthPasswordResetCode({ email });
      if (response.status === 204) {
        setCodeSent(true);
        // 「已受理」而不是「已发送」：不存在的邮箱同样走这条路，
        // 写成「已发送到你的邮箱」等于替后端承诺了一件它没承诺的事。
        setNotice('如果这个邮箱注册过，验证码已经发出（5 分钟内有效）。');
        return;
      }
      setError((response.data as ApiError).message);
    } catch {
      setError('网络异常，请稍后再试');
    } finally {
      setBusy(false);
    }
  };

  const onSubmit = async (event: FormEvent) => {
    event.preventDefault();
    setError(null);
    // 与注册页同一口径：太短的口令服务端会拒，但等到往返回来才说太迟了——
    // 用户已经以为提交成功了，尤其这里还会把他的登录设备全部登出。
    const lengthError = passwordLengthError(password);
    if (lengthError) {
      setError(lengthError);
      return;
    }
    if (password !== confirm) {
      setError('两次输入的密码不一致');
      return;
    }
    setBusy(true);
    try {
      const response = await postAuthPasswordReset({ email, code, newPassword: password });
      if (response.status === 204) {
        navigate(`/login?reset=1${returnToParam(rawReturnTo, '&')}`, { replace: true });
        return;
      }
      const body = response.data as ApiError;
      // 401 不区分「码错了 / 过期了 / 试太多次 / 邮箱不存在」——后端刻意合并，
      // 前端原样转述后端的话，不要自己拆开猜。
      setError(body.message);
    } catch {
      setError('网络异常，请稍后再试');
    } finally {
      setBusy(false);
    }
  };

  return (
    <section className="mx-auto w-full max-w-md px-4 py-10">
      <h1 className="text-2xl font-semibold text-gray-900 dark:text-gray-100">找回密码</h1>
      <p className="mt-2 text-sm text-gray-600 dark:text-gray-400">
        用主邮箱收一个验证码，重设之后这台账号此前登录的所有设备都会退出。
      </p>

      <form className="mt-6 space-y-4" onSubmit={onSubmit}>
        <div className="flex items-end gap-2">
          <div className="flex-1">
            <Input
              label="邮箱"
              type="email"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              autoComplete="email"
              required
            />
          </div>
          <Button type="button" onClick={() => void requestCode()} disabled={busy || email === ''}>
            获取验证码
          </Button>
        </div>

        {codeSent ? (
          <>
            <Input
              label="验证码"
              value={code}
              onChange={(e) => setCode(e.target.value)}
              inputMode="numeric"
              autoComplete="one-time-code"
              required
            />
            <Input
              label={`新密码（至少 ${PASSWORD_MIN} 位）`}
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              autoComplete="new-password"
              required
            />
            <Input
              label="再输一次新密码"
              type="password"
              value={confirm}
              onChange={(e) => setConfirm(e.target.value)}
              autoComplete="new-password"
              required
            />
          </>
        ) : null}

        {notice ? (
          <p className="text-sm text-gray-600 dark:text-gray-400" role="status">
            {notice}
          </p>
        ) : null}
        {error ? (
          <p className="text-sm text-red-600 dark:text-red-400" role="alert">
            {error}
          </p>
        ) : null}

        <div className="flex items-center gap-3">
          <Button type="submit" primary disabled={busy || !codeSent}>
            {busy ? '处理中…' : '重设密码'}
          </Button>
          <Link
            to={`/login${returnToParam(rawReturnTo)}`}
            className="text-sm text-gray-600 hover:underline dark:text-gray-400"
          >
            想起来了，去登录
          </Link>
        </div>
      </form>
    </section>
  );
}
