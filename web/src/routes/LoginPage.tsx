import { useState, type FormEvent } from 'react';
import { Link, useNavigate, useSearchParams } from 'react-router';
import { Button, Input } from '@/components/ui';
import { postAuthLogin, type Error as ApiError } from '@/api';
import { useAuth } from '@/features/auth/AuthContext';
import { readReturnTo, returnToLabel, returnToParam } from '@/lib/return-to';

export function LoginPage() {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const { setMe } = useAuth();

  // 原样的参数留给下面几个链接继续往下传；校验过的值用来决定登录后去哪、
  // 以及页面上要不要写「登录后会回到 X」。两者不能混：拿原样的值去 navigate
  // 就是把校验绕过去了。
  const rawReturnTo = searchParams.get('return_to');
  const returnTo = readReturnTo(rawReturnTo);

  // 验证邮箱、找回密码处理完都把回执带到这里，让用户在登录页看到「上一步成了」。
  // 从前这两个参数写出去却没人读，用户验证完只看到一张干净的登录表单，
  // 分不清是自己走错了还是刚发生过什么。
  const verified = searchParams.get('verified') === '1';
  const reset = searchParams.get('reset') === '1';

  const [identifier, setIdentifier] = useState('');
  const [password, setPassword] = useState('');
  const [emailCode, setEmailCode] = useState('');
  const [needCode, setNeedCode] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  // 「邮箱还没验证」单独一位状态：它要的不是一句错误提示，而是一个能点的去处。
  // 混进 error 字符串里，用户只能看到「邮箱还没验证」然后无处可去。
  const [needVerify, setNeedVerify] = useState(false);

  const onSubmit = async (event: FormEvent) => {
    event.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const response = await postAuthLogin({
        identifier,
        password,
        emailCode: emailCode === '' ? undefined : emailCode,
      });
      if (response.status === 200) {
        setMe(response.data);
        navigate(returnTo, { replace: true });
        return;
      }
      const body = response.data as ApiError;
      // 失败次数到阈值时后端回 verification_required：不是「你密码错了」，
      // 而是「先证明这个邮箱是你的」。两种提示不能混，否则用户会一直重试密码。
      if (body.code === 'verification_required') {
        setNeedCode(true);
        setError('尝试次数较多，请填写邮箱验证码');
        return;
      }
      if (body.code === 'email_not_verified') {
        setNeedVerify(true);
        setError(body.message);
        return;
      }
      setError(body.message);
    } catch {
      setError('网络异常，请稍后再试');
    } finally {
      setBusy(false);
    }
  };

  return (
    <section className="mx-auto w-full max-w-sm px-4 py-12">
      <h1 className="text-2xl font-semibold">登录</h1>

      {(verified || reset) && (
        <p
          className="mt-4 rounded-lg border border-green-300 bg-green-50 p-3 text-sm text-green-800 dark:border-green-800 dark:bg-green-950 dark:text-green-200"
          role="status"
        >
          {reset
            ? '密码已经重设，请用新密码登录。此前的登录设备都已退出。'
            : '邮箱验证完成，现在可以登录了。'}
        </p>
      )}

      {/* 从投稿页被送到这里的人，需要知道登录不是终点。不说这一句，他就得靠
          「登录完居然回到了首页」自己猜出刚才那条路。 */}
      {returnTo !== '/' && (
        <p className="mt-2 text-sm text-gray-600 dark:text-gray-400" role="status">
          登录后会回到{returnToLabel(returnTo)}。
        </p>
      )}

      <form className="mt-6 flex flex-col gap-4" onSubmit={onSubmit}>
        <label className="flex flex-col gap-1 text-sm">
          邮箱或用户名
          <Input
            value={identifier}
            onChange={(e) => setIdentifier(e.target.value)}
            autoComplete="username"
            required
          />
        </label>

        <label className="flex flex-col gap-1 text-sm">
          密码
          <Input
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            autoComplete="current-password"
            required
          />
        </label>

        {needCode && (
          <label className="flex flex-col gap-1 text-sm">
            邮箱验证码
            <Input
              value={emailCode}
              onChange={(e) => setEmailCode(e.target.value)}
              inputMode="numeric"
              autoComplete="one-time-code"
              placeholder="6 位数字"
            />
          </label>
        )}

        {error && (
          <p className="m-0 text-sm text-red-600 dark:text-red-400" role="alert">
            {error}
          </p>
        )}
        {needVerify && (
          <Link
            to={`/verify-email?email=${encodeURIComponent(identifier)}${returnToParam(rawReturnTo, '&')}`}
            className="text-sm text-blue-600 dark:text-blue-400"
          >
            去验证邮箱
          </Link>
        )}

        <Button type="submit" disabled={busy}>
          {busy ? '登录中…' : '登录'}
        </Button>
      </form>

      <p className="mt-6 text-sm text-gray-500 dark:text-gray-400">
        还没有账号？
        <Link
          to={`/register${returnToParam(rawReturnTo)}`}
          className="text-blue-600 dark:text-blue-400"
        >
          注册
        </Link>
      </p>

      <p className="mt-2 text-sm text-gray-500 dark:text-gray-400">
        <Link
          to={`/password-reset${returnToParam(rawReturnTo)}`}
          className="text-blue-600 dark:text-blue-400"
        >
          忘记密码
        </Link>
      </p>
    </section>
  );
}
