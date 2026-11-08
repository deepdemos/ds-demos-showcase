import type { ReactElement } from 'react';
import { createBrowserRouter } from 'react-router';
import { DevRoomPage } from '@/dev-room/DevRoomPage';
import { DemoDetailPage } from '@/routes/DemoDetailPage';
import { EditDemoPage } from '@/routes/EditDemoPage';
import { DemosPage } from '@/routes/DemosPage';
import { HomePage } from '@/routes/HomePage';
import { LoginPage } from '@/routes/LoginPage';
import { RegisterPage } from '@/routes/RegisterPage';
import { PasswordResetPage } from '@/routes/PasswordResetPage';
import { VerifyEmailPage } from '@/routes/VerifyEmailPage';
import { SubmitPage } from '@/routes/SubmitPage';
import { AdminModelsPage } from '@/routes/AdminModelsPage';
import { ModelPage } from '@/routes/ModelPage';
import { RankingsPage } from '@/routes/RankingsPage';
import { AdminReviewPage } from '@/routes/AdminReviewPage';
import { NotFoundPage } from '@/routes/NotFoundPage';
import { AppLayout } from './AppLayout';
import { RouteError } from './RouteError';

const devRoutes = import.meta.env.DEV
  ? [{ path: '/dev-room', element: <DevRoomPage />, errorElement: <RouteError /> }]
  : [];

/**
 * 每条页面路由都挂一份兜底页。
 *
 * 实测（React Router 7）：只挂在顶层也能接住子路由的异常，但整棵树会被替换成
 * 错误页——导航、品牌、登录状态一起消失，用户除了刷新没有别的动作。逐条挂之后
 * 只有正文那一块被替换，外壳还在：「回首页」之外还能直接点去别的页面。
 *
 * 所以新增页面时用 `page()` 登记，别直接写裸的 `{ path, element }`。
 */
const page = (path: string, element: ReactElement) => ({
  path,
  element,
  errorElement: <RouteError />,
});

export const router = createBrowserRouter([
  ...devRoutes,
  {
    path: '/',
    element: <AppLayout />,
    // 这一份接的是 AppLayout 自己的异常，以及所有子路由 loader/action 抛出的错；
    // 页面渲染期崩掉由各自的 errorElement 接，外壳保持可用。
    errorElement: <RouteError />,
    children: [
      { index: true, element: <HomePage />, errorElement: <RouteError /> },
      page('demos', <DemosPage />),
      page('demos/:slug', <DemoDetailPage />),
      page('demos/:slug/edit', <EditDemoPage />),
      page('models/:slug', <ModelPage />),
      page('rankings', <RankingsPage />),
      page('login', <LoginPage />),
      page('register', <RegisterPage />),
      page('verify-email', <VerifyEmailPage />),
      page('password-reset', <PasswordResetPage />),
      page('submit', <SubmitPage />),
      page('admin/reviews', <AdminReviewPage />),
      page('admin/models', <AdminModelsPage />),
      page('*', <NotFoundPage />),
    ],
  },
]);
