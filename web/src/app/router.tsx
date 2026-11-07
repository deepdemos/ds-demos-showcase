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
import { AdminReviewPage } from '@/routes/AdminReviewPage';
import { NotFoundPage } from '@/routes/NotFoundPage';
import { AppLayout } from './AppLayout';

const devRoutes = import.meta.env.DEV ? [{ path: '/dev-room', element: <DevRoomPage /> }] : [];

export const router = createBrowserRouter([
  ...devRoutes,
  {
    path: '/',
    element: <AppLayout />,
    children: [
      { index: true, element: <HomePage /> },
      { path: 'demos', element: <DemosPage /> },
      { path: 'demos/:slug', element: <DemoDetailPage /> },
      { path: 'demos/:slug/edit', element: <EditDemoPage /> },
      { path: 'login', element: <LoginPage /> },
      { path: 'register', element: <RegisterPage /> },
      { path: 'verify-email', element: <VerifyEmailPage /> },
      { path: 'password-reset', element: <PasswordResetPage /> },
      { path: 'submit', element: <SubmitPage /> },
      { path: 'admin/reviews', element: <AdminReviewPage /> },
      { path: 'admin/models', element: <AdminModelsPage /> },
      { path: '*', element: <NotFoundPage /> },
    ],
  },
]);
