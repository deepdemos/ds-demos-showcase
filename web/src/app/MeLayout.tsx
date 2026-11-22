import { Navigate, Outlet, useLocation } from 'react-router';
import { NavList, Skeleton } from '@/components/ui';
import { useAuth } from '@/features/auth/AuthContext';
import { returnToParam } from '@/lib/return-to';
import { meNav } from './navigation';
import { RedirectPage } from './RedirectPage';

function scrollTop() {
  document.documentElement.scrollTop = 0;
  document.body.scrollTop = 0;
}

/**
 * 个人区外壳：一条二级导航，下面交给具体页面。
 *
 * 导航不再挂在每个页面里，页面只负责自己的抬头与内容。未登录时在布局层
 * 拦一次，页面里的守卫仍保留——单独测试页面时不用再套一层布局。
 */
export function MeLayout() {
  const { me, isLoading } = useAuth();
  const location = useLocation();

  // /me 本身没有内容：先落到默认标签，再让子布局的判断接手。
  if (location.pathname === '/me' || location.pathname === '/me/') {
    return <RedirectPage to="/me/demos" />;
  }

  if (isLoading) {
    return (
      <div className="mx-auto w-full max-w-3xl px-4 py-8 sm:px-6">
        <Skeleton lines={4} />
      </div>
    );
  }

  // HIST-001：本机浏览历史不要求登录；其余 /me 页面仍由布局守卫。
  const guestHistory = location.pathname === '/me/history';
  if (me === null && !guestHistory) {
    const returnTo = returnToParam(location.pathname + location.search + location.hash);
    return <Navigate to={'/login' + returnTo} replace />;
  }

  return (
    <div>
      {me ? (
        <div className="border-b border-gray-200 bg-white">
          <div className="mx-auto w-full max-w-6xl px-4 sm:px-6 lg:px-8">
            <NavList ariaLabel="个人中心导航" items={meNav(me)} onNavigate={scrollTop} />
          </div>
        </div>
      ) : null}
      <Outlet />
    </div>
  );
}
