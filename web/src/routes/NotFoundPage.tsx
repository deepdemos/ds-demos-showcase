import { Link } from 'react-router';

// 404 页曾经只有「404」和「页面不存在。」——用户除了手改地址栏没有别的路。
// 这里给两条：回作品库（大多数人的目的地）和回首页（不知道要去哪的人）。
const actionLink =
  'inline-flex items-center rounded-lg border border-gray-300 px-4 py-2 text-sm hover:border-gray-400 dark:border-gray-700 dark:hover:border-gray-600';

export function NotFoundPage() {
  return (
    <section className="mx-auto w-full max-w-3xl px-4 py-12">
      <h1 className="text-2xl font-semibold">404</h1>
      <p className="mt-2 text-sm text-gray-500 dark:text-gray-400">
        页面不存在。可能是地址写错了，也可能这个页面已经搬走。
      </p>
      <p className="mt-6 flex flex-wrap gap-3">
        <Link to="/demos" className={actionLink}>
          回作品库
        </Link>
        <Link to="/" className={actionLink}>
          回首页
        </Link>
      </p>
    </section>
  );
}
