import type { RouteObject } from 'react-router';
import { createBrowserRouter } from 'react-router';
import { DevRoomPage } from '@/dev-room/DevRoomPage';
import { AppLayout } from './AppLayout';
import { RouteError } from './RouteError';
import { routeElements } from './route-elements';
import { childPath, pages, type PageEntry } from './routes';

/**
 * 从页面清单生成路由。
 *
 * 这里只做三件事：按清单套外壳、给每条路由挂兜底页、把 404 放到最后。
 * 不要在这里import页面或手写 path——加页面去 routes.ts 登记，再在
 * route-elements.tsx 里补元素。
 */
function elementFor(page: PageEntry) {
  const element = routeElements[page.id];
  if (!element) throw new Error('页面清单缺少路由元素：' + page.id);
  return element;
}

function pageRoute(page: PageEntry): RouteObject {
  const base = { element: elementFor(page), errorElement: <RouteError /> };
  if (page.path === '*') return { path: '*', ...base };
  if (page.path === '/') return { index: true, ...base };
  return { path: childPath(page.path), ...base };
}

const appPages = pages.filter((page) => page.path !== '*');
const notFound = pages.find((page) => page.path === '*');

if (!notFound) throw new Error('页面清单缺少 404 路由');

/** DEV-only：组件开发室与后续页面清单。生产构建里整段被剔除。 */
const devRoutes: RouteObject[] = import.meta.env.DEV
  ? [{ path: '/dev-room', element: <DevRoomPage />, errorElement: <RouteError /> }]
  : [];

export const router = createBrowserRouter([
  ...devRoutes,
  {
    path: '/',
    element: <AppLayout />,
    // 这一份接 AppLayout 自己的异常，以及所有子路由 loader/action 抛出的错；
    // 页面渲染期崩掉由各自的 errorElement 接，外壳保持可用。
    errorElement: <RouteError />,
    children: [
      ...appPages.map(pageRoute),
      { path: '*', element: elementFor(notFound), errorElement: <RouteError /> },
    ],
  },
]);
