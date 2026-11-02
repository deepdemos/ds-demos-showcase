import { createBrowserRouter } from 'react-router';
import { DevRoomPage } from '@/dev-room/DevRoomPage';
import { DemoDetailPage } from '@/routes/DemoDetailPage';
import { DemosPage } from '@/routes/DemosPage';
import { HomePage } from '@/routes/HomePage';
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
      { path: '*', element: <NotFoundPage /> },
    ],
  },
]);
