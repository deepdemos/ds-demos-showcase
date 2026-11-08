import { Link } from 'react-router';
import { useAuth } from '@/features/auth/AuthContext';

// 首页只做一件事：把「这站是什么」和「现在能去哪」说清楚。
// 入口按登录态分叉——未登录给登录，已登录给投稿；两者都给的首页会把人劝走。
const actionLink =
  'inline-flex items-center rounded-lg border border-gray-300 px-4 py-2 text-sm hover:border-gray-400 dark:border-gray-700 dark:hover:border-gray-600';

export function HomePage() {
  const { me } = useAuth();

  return (
    <section className="mx-auto w-full max-w-3xl px-4 py-8">
      <h1 className="text-2xl font-semibold">DeepDemos 展示</h1>
      <p className="mt-2 text-sm text-gray-500 dark:text-gray-400">
        这里托管可运行的作品：网页作品或压缩包，审核通过后公开。可以按形态、技术、模型筛选作品库，看排行榜；登录后能投稿和评分。
      </p>
      <p className="mt-6 flex flex-wrap gap-3">
        <Link to="/demos" className={actionLink}>
          浏览作品库 →
        </Link>
        <Link to="/rankings" className={actionLink}>
          排行榜
        </Link>
        {me ? (
          <Link to="/submit" className={actionLink}>
            投稿
          </Link>
        ) : (
          <Link to="/login" className={actionLink}>
            登录
          </Link>
        )}
      </p>
    </section>
  );
}
