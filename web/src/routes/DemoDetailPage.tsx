import { useQuery } from '@tanstack/react-query';
import { Link, useParams } from 'react-router';
import { EmptyState, ErrorState, Skeleton } from '@/components/ui';
import { getDemo, type DemoDetail } from '@/api';

type Loaded = { kind: 'ok'; demo: DemoDetail } | { kind: 'missing' };

async function fetchDemo(slug: string): Promise<Loaded> {
  const response = await getDemo(slug);
  if (response.status === 200) return { kind: 'ok', demo: response.data };
  if (response.status === 404) return { kind: 'missing' };
  throw new Error(`作品加载失败（HTTP ${response.status}）`);
}

/**
 * 墓碑文案按「这个作品是怎么消失的」分三种。
 *
 * 标准对象 §ST-2 要求 archived 与 removed 的墓碑内容**必须不同**：一个是作者自己下架的，
 * 一个是平台移除的，混成一句「作品不存在」会让作者以为是被删了。
 * 还有第三种：作者删除（deleted_at 有值但生命周期仍是 published），它同样是 200 墓碑。
 */
export function tombstoneText(demo: Pick<DemoDetail, 'lifecycleState'>): {
  title: string;
  description: string;
} {
  if (demo.lifecycleState === 'archived') {
    return { title: '作者已下架这个作品', description: '作品不再公开展示。' };
  }
  if (demo.lifecycleState === 'removed') {
    return { title: '这个作品已被移除', description: '因违反社区规则，作品已从站内移除。' };
  }
  return { title: '作者已删除这个作品', description: '作品已被作者删除，不再公开展示。' };
}

export function DemoDetailPage() {
  const { slug = '' } = useParams();

  const { data, isPending, isError, error, refetch, isFetching } = useQuery({
    queryKey: ['demo', slug],
    queryFn: () => fetchDemo(slug),
    enabled: slug.length > 0,
  });

  return (
    <section className="mx-auto w-full max-w-3xl px-4 py-8">
      <p className="mb-4">
        <Link to="/demos" className="text-sm text-gray-500 hover:underline dark:text-gray-400">
          ← 返回作品库
        </Link>
      </p>

      {isPending && <Skeleton lines={4} lineHeight="h-8" />}

      {isError && (
        <ErrorState
          title="作品加载失败"
          description={error instanceof Error ? error.message : undefined}
          onRetry={() => void refetch()}
          retrying={isFetching}
        />
      )}

      {data?.kind === 'missing' && (
        <EmptyState title="作品不存在" description="它可能从未公开发布，或者已经被擦除。" />
      )}

      {data?.kind === 'ok' && data.demo.title === null && (
        <EmptyState {...tombstoneText(data.demo)} />
      )}

      {data?.kind === 'ok' && data.demo.title !== null && (
        <article>
          <h1 className="text-2xl font-semibold text-gray-900 dark:text-gray-100">
            {data.demo.title}
          </h1>
          <p className="mt-1 text-xs text-gray-500 dark:text-gray-400">
            发布 {new Date(data.demo.createdAt).toLocaleDateString('zh-CN')}
            {data.demo.updatedAt !== data.demo.createdAt &&
              ` · 更新 ${new Date(data.demo.updatedAt).toLocaleDateString('zh-CN')}`}
          </p>

          {data.demo.coverUrl ? (
            <img
              src={data.demo.coverUrl}
              alt=""
              className="mt-4 w-full rounded-xl border border-gray-200 object-cover dark:border-gray-800"
            />
          ) : (
            <div className="mt-4 flex h-48 items-center justify-center rounded-xl border border-dashed border-gray-200 bg-gray-50 text-sm text-gray-400 dark:border-gray-800 dark:bg-gray-900 dark:text-gray-500">
              这个作品没有封面
            </div>
          )}

          <p className="mt-4 whitespace-pre-line text-base leading-7 text-gray-700 dark:text-gray-300">
            {data.demo.description ?? '作者没有填写简介。'}
          </p>
        </article>
      )}
    </section>
  );
}
