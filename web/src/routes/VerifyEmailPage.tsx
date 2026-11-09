import { useState, type FormEvent } from 'react';
import { Link, useNavigate, useSearchParams } from 'react-router';
import { Button, Input } from '@/components/ui';
import { postAuthVerifyEmail, postAuthVerifyEmailResend, type Error as ApiError } from '@/api';
import { returnToParam } from '@/lib/return-to';

/**
 * 邮箱验证页。
 *
 * # 为什么是独立页面而不是注册页的第二屏
 *
 * 验证动作可以发生在「注册之后、换了台设备、隔了一天」——邮件里的链接或验证码
 * 打开的就是这一页。塞进注册页的第二屏，这条路就走不通了。
 *
 * # 为什么验证成功不自动登录
 *
 * 这一步证明的是「邮箱是我的」，不是「我是谁」。两个动作各自只做一件事：
 * 验证完跳登录页，用户自己登一次。多一次点击换来的是一条能对用户解释清楚的规则。
 */
export function VerifyEmailPage() {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  // 注册/登录那边带过来的落脚点，验证完继续往登录页传。
  const rawReturnTo = searchParams.get('return_to');

  const [email, setEmail] = useState(searchParams.get('email') ?? '');
  const [code, setCode] = useState('');
  const [busy, setBusy] = useState(false);
  const [resending, setResending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);

  // 注册页把「信到底发出去没有」通过查询参数带过来。注册响应里有这个事实，
  // 丢在这里就等于让用户对着一封不存在的邮件猜。
  const sendFailed = searchParams.get('sent') === '0';

  const onSubmit = async (event: FormEvent) => {
    event.preventDefault();
    setBusy(true);
    setError(null);
    setNotice(null);
    try {
      const response = await postAuthVerifyEmail({ email, code });
      if (response.status === 204) {
        navigate(`/login?verified=1${returnToParam(rawReturnTo, '&')}`, { replace: true });
        return;
      }
      setError((response.data as ApiError).message);
    } catch {
      setError('网络异常，请稍后再试');
    } finally {
      setBusy(false);
    }
  };

  const onResend = async () => {
    setResending(true);
    setError(null);
    setNotice(null);
    try {
      const response = await postAuthVerifyEmailResend({ email });
      // 204 是「已受理」，不代表信真的到了——不存在的邮箱也回 204。
      // 所以文案只说「如果这个邮箱注册过，信已经发出」，不承诺更多。
      if (response.status === 204) {
        setNotice('如果这个邮箱还没验证过，验证信已经发出。请检查收件箱与垃圾邮件。');
      } else {
        setError((response.data as ApiError).message);
      }
    } catch {
      setError('网络异常，请稍后再试');
    } finally {
      setResending(false);
    }
  };

  return (
    <section className="mx-auto w-full max-w-sm px-4 py-12">
      <h1 className="text-2xl font-semibold">验证邮箱</h1>
      <p className="mt-1 text-sm text-gray-500 dark:text-gray-400">
        我们给你的邮箱发了一个 6 位验证码。验证之后就能登录了。
      </p>

      {sendFailed && (
        <p
          className="mt-4 rounded-lg border border-amber-300 bg-amber-50 p-3 text-sm text-amber-800 dark:border-amber-800 dark:bg-amber-950 dark:text-amber-200"
          role="status"
        >
          账号已经建好了，但验证信没发出去。点下面的「重新发送」再试一次。
        </p>
      )}

      <form className="mt-6 flex flex-col gap-4" onSubmit={onSubmit}>
        <label className="flex flex-col gap-1 text-sm">
          邮箱
          <Input
            type="email"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            autoComplete="email"
            required
          />
        </label>

        <label className="flex flex-col gap-1 text-sm">
          验证码
          <Input
            value={code}
            onChange={(e) => setCode(e.target.value)}
            inputMode="numeric"
            autoComplete="one-time-code"
            maxLength={6}
            required
          />
        </label>

        {error && (
          <p className="m-0 text-sm text-red-600 dark:text-red-400" role="alert">
            {error}
          </p>
        )}
        {notice && (
          <p className="m-0 text-sm text-green-700 dark:text-green-400" role="status">
            {notice}
          </p>
        )}

        <Button type="submit" disabled={busy}>
          {busy ? '验证中…' : '完成验证'}
        </Button>
      </form>

      <div className="mt-4 flex items-center justify-between text-sm">
        <Button
          type="button"
          inline
          onClick={() => void onResend()}
          disabled={resending || email === ''}
        >
          {resending ? '发送中…' : '重新发送验证码'}
        </Button>
        <Link
          to={`/login${returnToParam(rawReturnTo)}`}
          className="text-gray-500 dark:text-gray-400"
        >
          去登录
        </Link>
      </div>
    </section>
  );
}
