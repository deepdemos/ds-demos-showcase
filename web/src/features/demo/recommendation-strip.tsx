import { useQuery } from '@tanstack/react-query';
import { Link } from 'react-router';
import { listRecommendations } from '@/api';

/**
 * 「为你推荐」条。
 *
 * 只在登录后才可能有内容，而「未登录」与「还没有推荐」在界面上是同一种处置：
 * 整块不渲染。区别在接口上（401 vs 空数组），不在界面上——给访客看一句
 * 「登录后为你推荐」既占地方又泄了「我们知道你是谁」的错觉。
 *
 * 放在列表页顶部而不是详情页：详情页已经有「相关作品」，那是「这件作品旁边有什么」，
 * 这里是「你可能会想看的」，两件事，别塞进同一个区块。
 */
export function RecommendationStrip() {
  const query = useQuery({
    queryKey: ['recommendations'],
    queryFn: async () => {
      const res = await listRecommendations({ limit: 6 });
      if (res.status !== 200) throw new Error(`推荐加载失败（HTTP ${res.status}）`);
      return res.data.items;
    },
    // 现算的改动频次很低：浏览一件作品就可能改一次排序，但那不值得每次进列表页都重算。
    staleTime: 5 * 60 * 1000,
    retry: false,
  });

  const items = query.data ?? [];
  if (query.isError || items.length === 0) return null;

  return (
    <section className="mb-6" aria-labelledby="recommendation-heading">
      <h2
        id="recommendation-heading"
        className="mb-2 text-sm font-semibold text-gray-700 dark:text-gray-300"
      >
        为你推荐
      </h2>
      <ul className="grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
        {items.map((item) => (
          <li key={item.slug}>
            <Link
              to={`/demos/${item.slug}`}
              className="block rounded-lg border border-gray-200 px-3 py-2 text-sm hover:border-gray-400 dark:border-gray-800 dark:hover:border-gray-600"
            >
              <span className="font-medium">{item.title}</span>
              {item.classifications.length > 0 ? (
                <span className="mt-1 block text-xs text-gray-500 dark:text-gray-400">
                  {item.classifications.map((c) => c.label).join(' · ')}
                </span>
              ) : null}
            </Link>
          </li>
        ))}
      </ul>
    </section>
  );
}
