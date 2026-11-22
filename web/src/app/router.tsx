import type { RouteObject } from 'react-router';
import { createBrowserRouter } from 'react-router';
import { DevRoomPage } from '@/dev-room/DevRoomPage';
import { AdminLayout } from './AdminLayout';
import { AppShell } from './AppShell';
import { AUTH } from './auth-layout';
import { MeLayout } from './MeLayout';
import { RedirectPage } from './RedirectPage';
import { RouteError } from './RouteError';
import { routeElements } from './route-elements';
import { childPath, pages, redirects, type PageEntry } from './routes';

/**
 * 从页面清单生成路由。
 *
 * 这里只做四件事：按区套外壳、给每条路由挂兜底页、接旧地址跳转、把 404
 * 放到最后。不要在这里手写页面 path——加页面去 routes.ts 登记，再在
 * route-elements.tsx 里补元素。
 */
function elementFor(page: PageEntry) {
  const element = routeElements[page.id];
  if (!element) throw new Error('页面清单缺少路由元素：' + page.id);
  return element;
}

function pageRoute(page: PageEntry, parentPath = ''): RouteObject {
  const base = { element: elementFor(page), errorElement: <RouteError /> };
  if (page.path === '*') return { path: '*', ...base };
  const path = parentPath
    ? page.path.slice(parentPath.length).replace(/^\//, '')
    : childPath(page.path);
  if (page.path === '/') return { index: true, ...base };
  return { path, ...base };
}

const notFound = pages.find((page) => page.path === '*');
if (!notFound) throw new Error('页面清单缺少 404 路由');

const authPages = pages.filter((page) => page.group === 'auth');
const mePages = pages.filter((page) => page.group === 'me');
const adminPages = pages.filter((page) => page.group === 'admin');
const shellPages = pages.filter(
  (page) => (page.group === 'public' || page.group === 'system') && page.path !== '*',
);

/** 旧地址：和页面一样套 AppShell，跳转时保留查询串。 */
const redirectRoutes: RouteObject[] = redirects.map((entry) => ({
  path: childPath(entry.from),
  element: <RedirectPage to={entry.to} />,
  errorElement: <RouteError />,
}));

/** DEV-only：组件开发室与后续页面清单。生产构建里整段被剔除。 */
const devRoutes: RouteObject[] = import.meta.env.DEV
  ? [{ path: '/dev-room', element: <DevRoomPage />, errorElement: <RouteError /> }]
  : [];

export const router = createBrowserRouter([
  ...devRoutes,
  {
    path: '/',
    element: <AppShell />,
    // 这一份接 AppShell 自己的异常，以及所有子路由 loader/action 抛出的错；
    // 页面渲染期崩掉由各自的 errorElement 接，外壳保持可用。
    errorElement: <RouteError />,
    children: [
      ...shellPages.map((page) => pageRoute(page)),
      {
        path: 'me',
        element: <MeLayout />,
        errorElement: <RouteError />,
        children: mePages.map((page) => pageRoute(page, '/me')),
      },
      {
        path: 'admin',
        element: <AdminLayout />,
        errorElement: <RouteError />,
        children: adminPages.map((page) => pageRoute(page, '/admin')),
      },
      ...redirectRoutes,
      pageRoute(notFound),
    ],
  },
  {
    path: '/',
    element: <AUTH />,
    errorElement: <RouteError />,
    children: authPages.map((page) => pageRoute(page)),
  },
]);
