import { useState, type FormEvent } from 'react';
import { Link, useNavigate } from 'react-router';
import { Button, Input } from '@/components/ui';
import { postAuthRegister, type Error as ApiError } from '@/api';

// 与后端同口径的最小校验。前端校验只为省一次往返，真正的判定在服务端——
// 这里放过的值服务端还会再拒一次，所以不必追求完备。
const USERNAME_RE = /^[A-Za-z0-9_-]{3,20}$/;
const PASSWORD_MIN = 10;

export function RegisterPage() {
  const navigate = useNavigate();

  const [email, setEmail] = useState('');
  const [username, setUsername] = useState('');
  const [displayName, setDisplayName] = useState('');
  const [password, setPassword] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const usernameError =
    username !== '' && !USERNAME_RE.test(username)
      ? '用户名需 3–20 位，只能用字母、数字、下划线和连字符'
      : undefined;
  const passwordError =
    password !== '' && password.length < PASSWORD_MIN ? `密码至少 ${PASSWORD_MIN} 位` : undefined;

  const onSubmit = async (event: FormEvent) => {
    event.preventDefault();
    if (usernameError || passwordError) return;
    setBusy(true);
    setError(null);
    try {
      const response = await postAuthRegister({
        email,
        username,
        displayName: displayName === '' ? username : displayName,
        password,
      });
      if (response.status === 201) {
        // 注册不再直接登录（账号是 `pending`，要先去验证邮箱）。
        // `sent=0` 把「信没发出去」这个事实带到验证页——否则用户会对着一个
        // 永远不来的邮件等下去。
        const query = response.data.verificationSent ? '' : '&sent=0';
        navigate(`/verify-email?email=${encodeURIComponent(response.data.email)}${query}`, {
          replace: true,
        });
        return;
      }
      setError((response.data as ApiError).message);
    } catch {
      setError('网络异常，请稍后再试');
    } finally {
      setBusy(false);
    }
  };

  return (
    <section className="mx-auto w-full max-w-sm px-4 py-12">
      <h1 className="text-2xl font-semibold">注册</h1>
      <p className="mt-1 text-sm text-gray-500 dark:text-gray-400">
        注册后需要先验证邮箱，验证完才能登录。投稿内容需要经过审核才会公开。
      </p>

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
          用户名
          <Input
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            error={usernameError}
            autoComplete="username"
            required
          />
        </label>

        <label className="flex flex-col gap-1 text-sm">
          显示名（可留空，默认用用户名）
          <Input value={displayName} onChange={(e) => setDisplayName(e.target.value)} />
        </label>

        <label className="flex flex-col gap-1 text-sm">
          密码（至少 {PASSWORD_MIN} 位）
          <Input
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            error={passwordError}
            autoComplete="new-password"
            required
          />
        </label>

        {error && (
          <p className="m-0 text-sm text-red-600 dark:text-red-400" role="alert">
            {error}
          </p>
        )}

        <Button type="submit" disabled={busy}>
          {busy ? '注册中…' : '注册'}
        </Button>
      </form>

      <p className="mt-6 text-sm text-gray-500 dark:text-gray-400">
        已有账号？
        <Link to="/login" className="text-blue-600 dark:text-blue-400">
          登录
        </Link>
      </p>
    </section>
  );
}
