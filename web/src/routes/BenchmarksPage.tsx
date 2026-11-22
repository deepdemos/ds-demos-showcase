import { Link } from 'react-router';
import { useQuery } from '@tanstack/react-query';
import { EmptyState, ErrorState, Skeleton } from '@/components/ui';
import { Page } from '@/components/layout/page';
import { listBenchmarks, type BenchmarkSummary } from '@/api';

/**
 * 第三方基准目录（模型域 §5.6）。
 *
 * 只做分榜陈列：这里回答「有哪些榜」，不回答「谁比谁强」。跨榜归一化与综合分是后续层，
 * 所以页面上不出现任何「总分」或「即将上线」的占位。
 */

const TIER_LABEL: Record<BenchmarkSummary['displayTier'], string> = {
  table: '有分数',
  cite: '仅引用',
  link: '仅链接',
};

function formatDay(value: string | null | undefined): string {
  if (!value) return '—';
  return new Date(value).toLocaleDateString('zh-CN');
}

export function BenchmarksPage() {
  const query = useQuery({
    queryKey: ['benchmarks'],
    queryFn: async (): Promise<BenchmarkSummary[]> => {
      const response = await listBenchmarks();
      if (response.status !== 200) throw new Error('基准目录加载失败（' + response.status + '）');
      return response.data.items;
    },
  });

  return (
    <Page
      width="wide"
      title="第三方基准"
      description={'站内已收录的第三方基准。每张榜单独陈列：只给原始分与名次，不跨榜比较。'}
    >
      {query.isPending ? (
        <div className="mt-6">
          <Skeleton lines={5} />
        </div>
      ) : query.isError ? (
        <div className="mt-6">
          <ErrorState
            title="基准目录加载失败"
            description={query.error instanceof Error ? query.error.message : '请稍后重试'}
            onRetry={() => void query.refetch()}
            retrying={query.isFetching}
          />
        </div>
      ) : query.data.length === 0 ? (
        <div className="mt-6">
          <EmptyState
            title="还没有接入任何基准榜"
            description="站方导入数据后，这里会按名字列出每一张榜。"
          />
        </div>
      ) : (
        <div className="mt-6 overflow-x-auto">
          <table className="w-full min-w-[40rem] border-collapse text-sm">
            <thead>
              <tr className="border-b border-gray-200 text-left text-xs text-gray-500">
                <th className="py-2 pr-3 font-medium">榜单</th>
                <th className="py-2 pr-3 font-medium">发布方</th>
                <th className="py-2 pr-3 font-medium">方向</th>
                <th className="py-2 pr-3 font-medium">最新版本</th>
                <th className="py-2 font-medium">陈列档位</th>
              </tr>
            </thead>
            <tbody>
              {query.data.map((item) => (
                <tr key={item.slug} className="border-b border-gray-100">
                  <td className="py-2 pr-3">
                    <Link
                      to={'/benchmarks/' + item.slug}
                      className="text-gray-900 underline-offset-2 hover:underline"
                    >
                      {item.name}
                    </Link>
                  </td>
                  <td className="py-2 pr-3 text-gray-600">{item.publisher || '—'}</td>
                  <td className="py-2 pr-3 text-gray-600">{item.category || 'general'}</td>
                  <td className="py-2 pr-3 text-gray-600">{formatDay(item.latestAsOf)}</td>
                  <td className="py-2 text-gray-600">{TIER_LABEL[item.displayTier]}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </Page>
  );
}
