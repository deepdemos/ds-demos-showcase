import { Link, Outlet } from 'react-router';
import { RouteTitle } from './RouteTitle';

/**
 * 认证壳：登录 / 注册 / 验证邮箱 / 重置密码。
 *
 * 这四页不套主导航：用户还没登录，给他一排可能去不了的入口只会制造
 * 一次无意义的跳转。保留品牌与“回作品库”一条退路。
 */
export function AUTH() {
  return (
    <div className="flex min-h-screen flex-col bg-gray-50 text-gray-900">
      <RouteTitle />
      <header className="border-b border-gray-200 bg-white">
        <div className="mx-auto flex min-h-14 w-full max-w-6xl items-center justify-between gap-4 px-4 py-2 sm:px-6 lg:px-8">
          <Link
            to="/"
            className="whitespace-nowrap text-base font-semibold text-gray-900 no-underline"
          >
            DeepDemos 展示
          </Link>
          <Link className="text-sm text-gray-600 underline" to="/demos">
            回作品库
          </Link>
        </div>
      </header>
      <main id="main" className="flex-1">
        <Outlet />
      </main>
      <footer className="border-t border-gray-200 bg-white">
        <div className="mx-auto w-full max-w-6xl px-4 py-3 text-xs text-gray-500 sm:px-6 lg:px-8">
          <Link className="underline" to="/community-guidelines">
            社区规范
          </Link>
        </div>
      </footer>
    </div>
  );
}
