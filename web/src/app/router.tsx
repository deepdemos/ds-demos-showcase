import { createBrowserRouter } from 'react-router';
import { DevRoomPage } from '@/dev-room/DevRoomPage';
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
      { path: '*', element: <NotFoundPage /> },
    ],
  },
]);
