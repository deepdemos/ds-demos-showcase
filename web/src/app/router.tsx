import type { RouteObject } from 'react-router';
import { createBrowserRouter, matchPath, useLocation } from 'react-router';
import { DevRoomPage } from '@/dev-room/DevRoomPage';
import { PagesListPage } from '@/dev-room/PagesListPage';
import { fixtureElements, fixturePages } from '@/dev-room/fixtures';
import { NotFoundPage } from '@/routes/NotFoundPage';
import { PageWidthProvider } from '@/components/layout/page';
import { UnavailablePage } from './UnavailablePage';
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
function elementFor(page: PageEntry, elements: typeof routeElements = routeElements) {
  const element = elements[page.id];
  if (!element) throw new Error('页面清单缺少路由元素：' + page.id);
  // width 的唯一消费者：页面自己再写一个也覆盖不了清单。
  return <PageWidthProvider width={page.width}>{element}</PageWidthProvider>;
}

function pageRoute(
  page: PageEntry,
  parentPath = '',
  elements: typeof routeElements = routeElements,
): RouteObject {
  const base = { element: elementFor(page, elements), errorElement: <RouteError /> };
  if (page.path === '*') return { path: '*', ...base };
  const path = parentPath
    ? page.path.slice(parentPath.length).replace(/^\//, '')
    : childPath(page.path);
  if (page.path === '/') return { index: true, ...base };
  return { path, ...base };
}

const notFound = pages.find((page) => page.path === '*');
if (!notFound) throw new Error('页面清单缺少 404 路由');

/** 生产访问 DEV-only fixture：给「尚未接入」，不要冒充普通 404。 */
function NotFoundRoute() {
  const { pathname } = useLocation();
  if (!import.meta.env.DEV) {
    const fixture = fixturePages.find(
      (page) => page.path !== '*' && matchPath({ path: page.path, end: true }, pathname) !== null,
    );
    if (fixture) return <UnavailablePage />;
  }
  return <NotFoundPage />;
}

const authPages = pages.filter((page) => page.group === 'auth');
const mePages = pages.filter((page) => page.group === 'me');
const adminPages = pages.filter((page) => page.group === 'admin');
const shellPages = pages.filter(
  (page) => (page.group === 'public' || page.group === 'system') && page.path !== '*',
);

/** DEV-only fixtures：路径和组挂在同一套外壳下，生产构建不注册。 */
const fixturePublic = import.meta.env.DEV
  ? fixturePages.filter((page) => page.group === 'public')
  : [];
const fixtureMe = import.meta.env.DEV ? fixturePages.filter((page) => page.group === 'me') : [];
const fixtureAdmin = import.meta.env.DEV
  ? fixturePages.filter((page) => page.group === 'admin')
  : [];

/** 旧地址：和页面一样套 AppShell，跳转时保留查询串。 */
const redirectRoutes: RouteObject[] = redirects.map((entry) => ({
  path: childPath(entry.from),
  element: <RedirectPage to={entry.to} />,
  errorElement: <RouteError />,
}));

/** DEV-only：组件开发室与后续页面清单。生产构建里整段被剔除。 */
const devRoutes: RouteObject[] = import.meta.env.DEV
  ? [
      { path: '/dev-room', element: <DevRoomPage />, errorElement: <RouteError /> },
      { path: '/dev-room/pages', element: <PagesListPage />, errorElement: <RouteError /> },
    ]
  : [];

export const routes: RouteObject[] = [
  ...devRoutes,
  {
    path: '/',
    element: <AppShell />,
    // 这一份接 AppShell 自己的异常，以及所有子路由 loader/action 抛出的错；
    // 页面渲染期崩掉由各自的 errorElement 接，外壳保持可用。
    errorElement: <RouteError />,
    children: [
      ...shellPages.map((page) => pageRoute(page)),
      ...fixturePublic.map((page) => pageRoute(page, '', fixtureElements)),
      {
        path: 'me',
        element: <MeLayout />,
        errorElement: <RouteError />,
        children: [
          ...mePages.map((page) => pageRoute(page, '/me')),
          ...fixtureMe.map((page) => pageRoute(page, '/me', fixtureElements)),
        ],
      },
      {
        path: 'admin',
        element: <AdminLayout />,
        errorElement: <RouteError />,
        children: [
          ...adminPages.map((page) => pageRoute(page, '/admin')),
          ...fixtureAdmin.map((page) => pageRoute(page, '/admin', fixtureElements)),
        ],
      },
      ...redirectRoutes,
      {
        path: '*',
        element: (
          <PageWidthProvider width={notFound.width}>
            <NotFoundRoute />
          </PageWidthProvider>
        ),
        errorElement: <RouteError />,
      },
    ],
  },
  {
    path: '/',
    element: <AUTH />,
    errorElement: <RouteError />,
    children: authPages.map((page) => pageRoute(page)),
  },
];

export const router = createBrowserRouter(routes);
