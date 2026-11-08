import { useQuery } from '@tanstack/react-query';
import { Link } from 'react-router';
import { listRelatedDemos } from '@/api';

/**
 * 相关作品（内容信号：共用分类 / 共用模型 / 同一作者）。
 *
 * 三个刻意的选择：
 * - **读不到或为空就整块不渲染**。相关作品是锦上添花，它不该在详情页上留一个
 *   「加载失败」或「暂无相关作品」的空框——读者对一个空框的注意力成本高于收益。
 * - **不显示「因为共用 3D建模」**。信号是排序依据，不是审核结论；把理由写出来
 *   会让读者以为我们在给作品归类背书。
 * - **不按流量排**。共同浏览要等浏览数据攒够，而分类与模型是投稿时就写下的。
 */
export function RelatedSection({ slug }: { slug: string }) {
  const query = useQuery({
    queryKey: ['related', slug],
    queryFn: async () => {
      const response = await listRelatedDemos(slug);
      if (response.status !== 200) throw new Error(`相关作品加载失败（HTTP ${response.status}）`);
      return response.data;
    },
    // 相关作品与「当前页面是谁」绑定：同一个 slug 反复进出不必重算。
    staleTime: 5 * 60 * 1000,
    retry: false,
  });

  const items = query.data?.items ?? [];
  if (query.isError || items.length === 0) return null;

  return (
    <section className="mt-8" aria-labelledby="related-heading">
      <h2 id="related-heading" className="text-lg font-semibold">
        相关作品
      </h2>
      <ul className="mt-3 grid gap-3 sm:grid-cols-2">
        {items.map((item) => (
          <li key={item.slug}>
            <Link
              to={`/demos/${item.slug}`}
              className="block rounded-lg border border-gray-200 p-3 hover:border-gray-400 dark:border-gray-800 dark:hover:border-gray-600"
            >
              <span className="font-medium text-gray-900 dark:text-gray-100">
                {item.title ?? item.slug}
              </span>
              {item.classifications?.length ? (
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
