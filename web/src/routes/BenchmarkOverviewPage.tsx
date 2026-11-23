import { Link } from 'react-router';
import { useQuery } from '@tanstack/react-query';
import { EmptyState, ErrorState, Skeleton } from '@/components/ui';
import { Page } from '@/components/layout/page';
import {
  getBenchmarkOverview,
  type BenchmarkOverview,
  type BenchmarkOverviewColumn,
  type BenchmarkOverviewRow,
} from '@/api';
import { categoryLabel } from '@/content/benchmark-sources';
import { tierLabel } from '@/content/benchmark-tier';

/**
 * 模型 × 基准总览（`/benchmarks/overview`，#571 / #572）。
 *
 * 一屏回答「同一个模型在几张榜上各落在哪」：一行一个模型、一列一张榜，
 * 格子是**原始分 + 榜内名次**。三条口径写在页面上，不写在注释里：
 * - 只有 `table` 档的榜进矩阵：`cite` / `link` 档没有分数可展示。
 * - 缺格显示 `—`，**不补零**：没测过不等于得了 0 分。
 * - 不归一化、不合成总分、不做跨列颜色：不同量纲的分数没有可比性（§5.3 / §5.4 是后续层）。
 *
 * 阅读规则（#572）由服务端给：列按能力组（coding → agent → general → math → other）
 * 再按榜名，行按有格子的列数降序再按模型名。页面**只渲染数组顺序，不再排一次**——
 * 前端排一遍等于给排序规则开第二个出处，两处迟早不一致。
 *
 * 行列上限也在服务端（列 24 / 行 200），超限时响应带回截断前的总数：页面据此
 * 写明「只显示前 N 列 / 行」，**不静默截断**。静默截断会让读者以为这就是全部。
 */

function formatScore(value: number): string {
  return Number.isInteger(value) ? String(value) : String(Math.round(value * 100) / 100);
}

function rowCells(row: BenchmarkOverviewRow): Map<string, BenchmarkOverviewRow['cells'][number]> {
  return new Map(row.cells.map((cell) => [cell.benchmarkSlug, cell]));
}

interface ColumnGroup {
  category: BenchmarkOverviewColumn['category'];
  label: string;
  columns: BenchmarkOverviewColumn[];
}

/** 按服务端给的列序切段：同一组相邻的列合并成一个 colspan，不做排序。 */
function groupColumns(columns: BenchmarkOverviewColumn[]): ColumnGroup[] {
  const groups: ColumnGroup[] = [];
  for (const column of columns) {
    const last = groups[groups.length - 1];
    if (last && last.category === column.category) {
      last.columns.push(column);
    } else {
      groups.push({
        category: column.category,
        label: categoryLabel[column.category] ?? column.category,
        columns: [column],
      });
    }
  }
  return groups;
}

/** 超限提示。正常矩阵不出现这段文字，超限时把「少看了多少」写在表头上方。 */
function TruncationNotice({ data }: { data: BenchmarkOverview }) {
  const columnsClipped = data.totalColumns > data.columns.length;
  const rowsClipped = data.totalRows > data.rows.length;
  if (!columnsClipped && !rowsClipped) return null;

  const parts: string[] = [];
  if (columnsClipped) {
    parts.push('列只显示前 ' + data.columns.length + ' 列（共 ' + data.totalColumns + ' 列）');
  }
  if (rowsClipped) {
    parts.push('行只显示前 ' + data.rows.length + ' 行（共 ' + data.totalRows + ' 行）');
  }

  return (
    <p role="status" className="mt-6 text-sm text-gray-600">
      矩阵有行列上限，超出的部分不展示：{parts.join('；')}。
    </p>
  );
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
        '缺格是没测过，不是 0 分。跨榜不比较、不归一化。' +
        '列按能力象限分组、行按覆盖的榜数排序；超出上限时这里会写明只显示前几列 / 行。'
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
        <>
          <TruncationNotice data={query.data} />
          {/* 窄屏横向滚动；限高让表头与首列的 sticky 真的有东西可吸。 */}
          <div className="mt-6 max-h-[70vh] overflow-auto">
            <table className="w-full min-w-[48rem] border-collapse text-sm">
              <thead>
                <tr className="border-b border-gray-200 text-left text-xs text-gray-500">
                  {/* 首列与表头吸顶：四角单元格压在所有滚动内容之上。 */}
                  <th
                    scope="col"
                    rowSpan={2}
                    className="sticky top-0 left-0 z-30 bg-white py-2 pr-4 text-left font-medium"
                  >
                    模型
                  </th>
                  {groupColumns(query.data.columns).map((group) => (
                    <th
                      key={group.category}
                      scope="colgroup"
                      colSpan={group.columns.length}
                      className="py-2 pr-4 font-medium"
                    >
                      {group.label}
                    </th>
                  ))}
                </tr>
                <tr className="border-b border-gray-200 text-left text-xs text-gray-500">
                  {query.data.columns.map((column) => (
                    <th
                      key={column.benchmarkSlug}
                      scope="col"
                      className="sticky top-0 z-20 bg-white py-2 pr-4 font-medium"
                    >
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
                      <th
                        scope="row"
                        className="sticky left-0 z-10 bg-white py-2 pr-4 text-left font-normal"
                      >
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
        </>
      )}
    </Page>
  );
}
