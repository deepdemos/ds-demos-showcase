import { Navigate, Outlet, useLocation } from 'react-router';
import { NavList, Skeleton } from '@/components/ui';
import { useAuth } from '@/features/auth/AuthContext';
import { returnToParam } from '@/lib/return-to';
import { ForbiddenPage } from './ForbiddenPage';
import { ADMIN_NAV_GROUPS, adminNav, hasAdminAccess } from './navigation';
import { RedirectPage } from './RedirectPage';

const GROUPS = ADMIN_NAV_GROUPS;

function scrollTop() {
  document.documentElement.scrollTop = 0;
  document.body.scrollTop = 0;
}

/**
 * 管理区外壳：左侧分组导航，右侧页面。
 *
 * 管理区不是一个页面，而是一组页面；把「有没有管理权限」放在布局层，
 * 页面自己的 403/登录分支仍保留，单独测试页面时不依赖这里。
 */
export function AdminLayout() {
  const { me, isLoading } = useAuth();
  const location = useLocation();

  // /admin 本身没有内容：先落到收件箱，再让子布局的判断接手。
  if (location.pathname === '/admin' || location.pathname === '/admin/') {
    return <RedirectPage to="/admin/inbox" />;
  }

  if (isLoading) {
    return (
      <div className="mx-auto w-full max-w-4xl px-4 py-8 sm:px-6">
        <Skeleton lines={4} />
      </div>
    );
  }

  if (me === null) {
    const returnTo = returnToParam(location.pathname + location.search + location.hash);
    return <Navigate to={'/login' + returnTo} replace />;
  }

  if (!hasAdminAccess(me)) return <ForbiddenPage />;

  const items = adminNav(me);
  if (items.length === 0) return <ForbiddenPage />;

  return (
    <div className="flex w-full flex-col lg:flex-row">
      <aside className="hidden w-56 shrink-0 border-r border-gray-200 bg-white lg:block">
        <div className="px-3 py-6">
          <p className="px-2 pb-2 text-xs font-medium text-gray-500">管理</p>
          {GROUPS.map((group) => {
            const groupItems = items.filter((item) => item.group === group);
            if (groupItems.length === 0) return null;
            return (
              <div key={group} className="mb-4">
                <p className="px-2 pb-1 text-xs text-gray-400">{group}</p>
                <NavList
                  ariaLabel={'管理导航 - ' + group}
                  orientation="vertical"
                  items={groupItems}
                  onNavigate={scrollTop}
                />
              </div>
            );
          })}
        </div>
      </aside>

      <div className="min-w-0 flex-1">
        <div className="overflow-x-auto border-b border-gray-200 bg-white px-4 sm:px-6 lg:hidden">
          <NavList ariaLabel="管理导航（移动端）" items={items} onNavigate={scrollTop} />
        </div>
        <Outlet />
      </div>
    </div>
  );
}
