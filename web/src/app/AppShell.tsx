import { useQuery } from '@tanstack/react-query';
import { useEffect, useState } from 'react';
import { Link, Outlet, useLocation, useNavigate } from 'react-router';
import { getUnreadNotificationCount } from '@/api';
import { Button, Input, Menu, NavList, type MenuItem } from '@/components/ui';
import { useAuth } from '@/features/auth/AuthContext';
import { primaryNav, userNav } from './navigation';
import { RouteTitle } from './RouteTitle';

/**
 * 站点外壳：品牌 + 一级导航 + 搜索位 + 用户区 + 页脚。
 *
 * 搜索位在第 5 批（/search 页面）接入；现在不渲染一个提交后 404 的输入框。
 * 页脚用 flex 列贴底，短页不会停在屏幕中间。
 */
export function AppShell() {
  const { me, isLoading, signOut } = useAuth();
  const navigate = useNavigate();
  const location = useLocation();
  const [mobileOpen, setMobileOpen] = useState(false);
  const [searchDraft, setSearchDraft] = useState('');

  const unread = useQuery({
    queryKey: ['notifications', 'unread'],
    queryFn: async () => {
      const response = await getUnreadNotificationCount();
      if (response.status !== 200) throw new Error('未读数加载失败');
      return response.data.count;
    },
    enabled: me !== null,
    refetchInterval: 60_000,
  });

  const primary = primaryNav(me);
  const user = userNav(me).map((item) =>
    item.id === 'notifications' && unread.data ? { ...item, badge: unread.data } : item,
  );

  useEffect(() => {
    setMobileOpen(false);
    document.documentElement.scrollTop = 0;
    document.body.scrollTop = 0;
  }, [location.pathname]);

  useEffect(() => {
    if (!mobileOpen) return;
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setMobileOpen(false);
    };
    document.addEventListener('keydown', onKeyDown);
    return () => document.removeEventListener('keydown', onKeyDown);
  }, [mobileOpen]);

  const current = location.pathname + location.search;
  const loginTo = '/login?return_to=' + encodeURIComponent(current);

  function submitSearch(event: React.FormEvent) {
    event.preventDefault();
    const q = searchDraft.trim();
    navigate(q === '' ? '/search' : '/search?q=' + encodeURIComponent(q));
    setMobileOpen(false);
  }

  const menuItems: MenuItem[] = [
    ...user.map((item) => ({ id: item.id, label: item.label, to: item.to, badge: item.badge })),
    {
      id: 'logout',
      label: '登出',
      danger: true,
      onSelect: () => void signOut().then(() => navigate('/')),
    },
  ];

  return (
    <div className="flex min-h-screen flex-col bg-gray-50 text-gray-900">
      <RouteTitle />
      <a
        href="#main"
        className="sr-only rounded bg-white px-3 py-2 text-sm focus:not-sr-only focus:absolute focus:left-2 focus:top-2 focus:z-30"
      >
        跳到正文
      </a>
      <header className="border-b border-gray-200 bg-white">
        <div className="mx-auto flex min-h-14 w-full max-w-6xl items-center gap-4 px-4 py-2 sm:px-6 lg:px-8">
          <Link
            to="/"
            className="shrink-0 whitespace-nowrap text-base font-semibold text-gray-900 no-underline"
          >
            DeepDemos 展示
          </Link>

          <NavList
            ariaLabel="主导航"
            items={primary}
            onNavigate={() => setMobileOpen(false)}
            className="hidden min-w-0 lg:flex"
          />

          <form className="hidden min-w-0 lg:block" onSubmit={submitSearch} role="search">
            <div className="flex w-56 items-center gap-2">
              <Input
                fullWidth
                type="search"
                value={searchDraft}
                onChange={(event) => setSearchDraft(event.target.value)}
                placeholder="搜索作品"
                aria-label="搜索作品"
              />
              <Button type="submit">搜索</Button>
            </div>
          </form>

          <div className="ml-auto hidden items-center gap-2 lg:flex">
            {isLoading ? (
              <span className="h-8 w-20 animate-pulse rounded bg-gray-100" aria-hidden="true" />
            ) : me ? (
              <Menu
                ariaLabel="用户菜单"
                label={me.displayName || me.username}
                items={menuItems}
                align="right"
                onOpenChange={(open) => {
                  if (open) setMobileOpen(false);
                }}
              />
            ) : (
              <Button onClick={() => navigate(loginTo)}>登录</Button>
            )}
          </div>

          <div className="ml-auto flex items-center gap-2 lg:hidden">
            <button
              type="button"
              aria-expanded={mobileOpen}
              aria-controls="mobile-nav"
              onClick={() => setMobileOpen((open) => !open)}
              className="rounded border border-gray-300 bg-white px-3 py-1.5 text-sm text-gray-800 hover:border-gray-400"
            >
              {mobileOpen ? '关闭' : '菜单'}
            </button>
          </div>
        </div>

        {mobileOpen ? (
          <div
            id="mobile-nav"
            className="border-t border-gray-200 px-4 pb-4 pt-2 sm:px-6 lg:hidden"
          >
            <form className="mb-3 flex items-end gap-2" onSubmit={submitSearch} role="search">
              <label className="flex flex-1 flex-col gap-1 text-sm">
                搜索
                <Input
                  fullWidth
                  type="search"
                  value={searchDraft}
                  onChange={(event) => setSearchDraft(event.target.value)}
                  placeholder="标题或简介里的词"
                  aria-label="搜索作品"
                />
              </label>
              <Button type="submit">搜索</Button>
            </form>
            <NavList
              ariaLabel="主导航（移动端）"
              items={primary}
              orientation="vertical"
              onNavigate={() => setMobileOpen(false)}
            />
            <div className="mt-3 border-t border-gray-200 pt-3">
              {isLoading ? (
                <span
                  className="block h-8 w-28 animate-pulse rounded bg-gray-100"
                  aria-hidden="true"
                />
              ) : me ? (
                <>
                  <NavList
                    ariaLabel="个人区"
                    items={user}
                    orientation="vertical"
                    onNavigate={() => setMobileOpen(false)}
                  />
                  <div className="mt-2">
                    <Button onClick={() => void signOut().then(() => navigate('/'))}>登出</Button>
                  </div>
                </>
              ) : (
                <Button onClick={() => navigate(loginTo)}>登录</Button>
              )}
            </div>
          </div>
        ) : null}
      </header>

      <main id="main" className="flex-1">
        <Outlet />
      </main>

      <footer className="border-t border-gray-200 bg-white">
        <div className="mx-auto flex w-full max-w-6xl flex-wrap items-center gap-x-4 gap-y-1 px-4 py-3 text-xs text-gray-500 sm:px-6 lg:px-8">
          <Link className="underline" to="/community-guidelines">
            社区规范
          </Link>
          <Link className="underline" to="/me/history">
            浏览历史
          </Link>
          {import.meta.env.DEV ? (
            <Link className="underline" to="/dev-room">
              开发房间
            </Link>
          ) : null}
        </div>
      </footer>
    </div>
  );
}
