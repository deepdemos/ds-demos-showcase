import { Link } from 'react-router';
import { useQuery } from '@tanstack/react-query';
import { EmptyState, ErrorState, Skeleton } from '@/components/ui';
import { Page } from '@/components/layout/page';
import { getBenchmarkOverview, type BenchmarkOverview, type BenchmarkOverviewRow } from '@/api';
import { tierLabel } from '@/content/benchmark-tier';

/**
 * 模型 × 基准总览（`/benchmarks/overview`，#571）。
 *
 * 一屏回答「同一个模型在几张榜上各落在哪」：一行一个模型、一列一张榜，
 * 格子是**原始分 + 榜内名次**。三条口径写在页面上，不写在注释里：
 * - 只有 `table` 档的榜进矩阵：`cite` / `link` 档没有分数可展示。
 * - 缺格显示 `—`，**不补零**：没测过不等于得了 0 分。
 * - 不归一化、不合成总分、不做跨列颜色：不同量纲的分数没有可比性（§5.3 / §5.4 是后续层）。
 *
 * 排序与上限由服务端给（#572），页面只负责渲染。
 */

function formatScore(value: number): string {
  return Number.isInteger(value) ? String(value) : String(Math.round(value * 100) / 100);
}

function rowCells(row: BenchmarkOverviewRow): Map<string, BenchmarkOverviewRow['cells'][number]> {
  return new Map(row.cells.map((cell) => [cell.benchmarkSlug, cell]));
}

export function BenchmarkOverviewPage() {
  const query = useQuery({
    queryKey: ['benchmarks', 'overview'],
    queryFn: async (): Promise<BenchmarkOverview> => {
      const response = await getBenchmarkOverview();
      if (response.status !== 200) throw new Error('基准总览加载失败（' + response.status + '）');
      return response.data;
    },
  });

  return (
    <Page
      title="基准总览"
      description={
        '一行一个模型、一列一张榜：格子是该榜的原始分与榜内名次。' +
        `只收「${tierLabel.table}」档的榜，` +
        '缺格是没测过，不是 0 分。跨榜不比较、不归一化。'
      }
      actions={
        <Link to="/benchmarks" className="text-sm underline">
          基准目录
        </Link>
      }
    >
      {query.isPending ? (
        <div className="mt-6">
          <Skeleton lines={6} />
        </div>
      ) : query.isError ? (
        <div className="mt-6">
          <ErrorState
            title="基准总览加载失败"
            description={query.error instanceof Error ? query.error.message : '请稍后重试'}
            onRetry={() => void query.refetch()}
            retrying={query.isFetching}
          />
        </div>
      ) : query.data.columns.length === 0 || query.data.rows.length === 0 ? (
        <div className="mt-6">
          <EmptyState
            title="还没有能放进矩阵的分数"
            description={
              `总览只收「${tierLabel.table}」档、有主指标、且已匹配到模型页的分数。` +
              '其余榜在基准目录里，按档位标明。'
            }
          />
        </div>
      ) : (
        <div className="mt-6 overflow-x-auto">
          <table className="w-full min-w-[48rem] border-collapse text-sm">
            <thead>
              <tr className="border-b border-gray-200 text-left text-xs text-gray-500">
                <th scope="col" className="py-2 pr-4 font-medium">
                  模型
                </th>
                {query.data.columns.map((column) => (
                  <th key={column.benchmarkSlug} scope="col" className="py-2 pr-4 font-medium">
                    <Link
                      to={'/benchmarks/' + column.benchmarkSlug}
                      className="text-gray-900 underline-offset-2 hover:underline"
                    >
                      {column.name}
                    </Link>
                    <span className="mt-0.5 block font-normal text-gray-500">
                      {column.unit ? column.unit + ' · ' : ''}
                      {column.version}
                    </span>
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {query.data.rows.map((row) => {
                const cells = rowCells(row);
                return (
                  <tr key={row.modelSlug} className="border-b border-gray-100">
                    <th scope="row" className="py-2 pr-4 text-left font-normal">
                      <Link
                        to={'/models/' + row.modelSlug}
                        className="text-gray-900 underline-offset-2 hover:underline"
                      >
                        {row.modelName}
                      </Link>
                    </th>
                    {query.data.columns.map((column) => {
                      const cell = cells.get(column.benchmarkSlug);
                      return (
                        <td key={column.benchmarkSlug} className="py-2 pr-4 text-gray-600">
                          {cell ? (
                            <>
                              {formatScore(cell.rawScore)}
                              {cell.rank === null ? null : (
                                <span className="ml-1 text-xs text-gray-500">#{cell.rank}</span>
                              )}
                            </>
                          ) : (
                            <span className="text-gray-400">—</span>
                          )}
                        </td>
                      );
                    })}
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
    </Page>
  );
}
