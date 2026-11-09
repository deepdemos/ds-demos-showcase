import { render, screen } from '@testing-library/react';
import { createMemoryRouter, Outlet, RouterProvider } from 'react-router';
import { beforeEach, expect, test, vi } from 'vitest';
import { RouteError } from './RouteError';
import { router } from './router';

// 兜底页自己也要被钉住：它是「出错时唯一还在的东西」，
// 一旦它渲染不出来，用户就真的只剩白屏了。
//
// 另一条同样重要：路由表里每一条都得挂上它。实测 React Router 7 的渲染期异常
// 只会被同一条路由的 errorElement 接住——漏挂的那一页崩了不会白屏整站，
// 但会留下「导航还在、正文空了」，比整页白屏更难判断。

function Boom(): never {
  throw new Error('boom-123');
}

/** 与 AppLayout 同形的外壳：有 Outlet 才会渲染子路由——没有它，子路由根本不会跑。 */
function Shell() {
  return (
    <div>
      外壳
      <Outlet />
    </div>
  );
}

beforeEach(() => {
  // React 会把渲染期异常再打一遍到 console，测试里不需要这段噪音。
  vi.spyOn(console, 'error').mockImplementation(() => {});
});

test('页面崩了渲染兜底页，导航外壳还在', async () => {
  const router = createMemoryRouter([
    {
      path: '/',
      element: <Shell />,
      errorElement: <RouteError />,
      children: [{ index: true, element: <Boom />, errorElement: <RouteError /> }],
    },
  ]);
  render(<RouterProvider router={router} />);

  expect(await screen.findByRole('heading', { name: '页面出错了' })).toBeInTheDocument();
  // 原始错误要露出来：用户截图报问题时，这行是唯一能定位的证据。
  expect(screen.getByRole('alert')).toHaveTextContent('boom-123');
  expect(screen.getByRole('button', { name: '重新加载' })).toBeInTheDocument();
  expect(screen.getByRole('link', { name: '回首页' })).toHaveAttribute('href', '/');
  // 外壳还在：逐条挂 errorElement 换来的就是这一点，用户还能直接去别的页面。
  expect(screen.getByText('外壳')).toBeInTheDocument();
});

test('loader 抛 Response 时显示状态码而不是空白', async () => {
  const router = createMemoryRouter([
    {
      path: '/',
      errorElement: <RouteError />,
      children: [
        {
          index: true,
          loader: () => {
            throw new Response('', { status: 503, statusText: 'Service Unavailable' });
          },
          element: <div>不该渲染到这里</div>,
        },
      ],
    },
  ]);
  render(<RouterProvider router={router} />);

  expect(await screen.findByRole('alert')).toHaveTextContent('503 Service Unavailable');
});

test('真实路由表里每一条都挂了 errorElement', () => {
  const root = router.routes.find((route) => route.path === '/');
  expect(root).toBeDefined();
  expect(root?.errorElement).toBeTruthy();

  const children = root?.children ?? [];
  expect(children.length).toBeGreaterThan(0);
  for (const child of children) {
    expect(child.errorElement, `路由 ${child.path ?? '(index)'} 缺 errorElement`).toBeTruthy();
  }
});
