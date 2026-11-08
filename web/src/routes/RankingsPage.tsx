import { useQuery } from '@tanstack/react-query';
import { Link, useSearchParams } from 'react-router';
import { Button, EmptyState, ErrorState, Select, Skeleton } from '@/components/ui';
import { listRankings } from '@/api';
import type { ListRankingsParams, RankingEntry } from '@/api';

/**
 * 作品榜（SOC-013 六口径 / SOC-016 三档时间范围）。
 *
 * 六个榜共用一个端点，所以这一页只有两处状态：`board` 与 `range`，都在 URL 里。
 * 「在 URL 里」不是为了好看——榜单是会被分享的东西（「你看上周的 5 分票榜」），
 * 而状态放在组件里就分享不出去，刷新也回不去。
 */

const BOARDS = ['avg', 'god', 'ghost', 'net', 'count', 'heat'] as const;
type Board = (typeof BOARDS)[number];

const RANGES = ['all', 'week', 'month'] as const;
type Range = (typeof RANGES)[number];

const BOARD_OPTIONS = [
  { value: 'avg', label: '平均分' },
  { value: 'god', label: '5 分票' },
  { value: 'ghost', label: '1 分票' },
  { value: 'net', label: '净票' },
  { value: 'count', label: '评分人数' },
  { value: 'heat', label: '综合热度' },
] as const;

const RANGE_OPTIONS = [
  { value: 'all', label: '全部时间' },
  { value: 'week', label: '近 7 天' },
  { value: 'month', label: '近 30 天' },
] as const;

/** 四个评分榜排除零评分作品（SOC-014），所以它们的空态与另外两个不一样。 */
const RATING_BOARDS: readonly Board[] = ['avg', 'god', 'ghost', 'net'];

/**
 * 每个榜的 `value` 含意不同：分数 / 票数 / 净票 / 热度分。
 *
 * 一律按「分」显示会读出「12.00 分」这种不可能的平均分——单位文案必须跟着口径走。
 */
function formatValue(board: Board, value: number): string {
  switch (board) {
    case 'avg':
      return `${value.toFixed(2)} 分`;
    case 'heat':
      return `热度 ${Math.round(value)}`;
    case 'net':
      return `净票 ${value > 0 ? '+' : ''}${Math.round(value)}`;
    default:
      return `${Math.round(value)} 票`;
  }
}

function readBoard(raw: string | null): Board {
  return BOARDS.includes(raw as Board) ? (raw as Board) : 'avg';
}

function readRange(raw: string | null): Range {
  return RANGES.includes(raw as Range) ? (raw as Range) : 'all';
}

function Row({ entry, board }: { entry: RankingEntry; board: Board }) {
  return (
    <li className="flex items-start gap-3 border-b border-gray-200 py-3 last:border-b-0 dark:border-gray-800">
      <span className="w-8 shrink-0 text-right text-sm font-semibold tabular-nums text-gray-500 dark:text-gray-400">
        {entry.rank}
      </span>
      <div className="min-w-0 flex-1">
        <Link
          to={`/demos/${entry.demo.slug}`}
          className="font-medium text-gray-900 hover:underline dark:text-gray-100"
        >
          {entry.demo.title ?? entry.demo.slug}
        </Link>
        {entry.demo.classifications?.length ? (
          <span className="ml-2 text-xs text-gray-500 dark:text-gray-400">
            {entry.demo.classifications.map((c) => c.label).join(' · ')}
          </span>
        ) : null}
      </div>
      <span className="shrink-0 text-sm tabular-nums text-gray-700 dark:text-gray-300">
        {formatValue(board, entry.value)}
      </span>
    </li>
  );
}

export function RankingsPage() {
  const [params, setParams] = useSearchParams();
  const board = readBoard(params.get('board'));
  const range = readRange(params.get('range'));

  const query = useQuery({
    queryKey: ['rankings', board, range],
    queryFn: async () => {
      // 只传非默认值：默认值也写进请求会让缓存键多分出一份
      // （`{board:'avg'}` 与 `{}` 是同一个榜，没必要两次请求）。
      const request: ListRankingsParams = {};
      if (board !== 'avg') request.board = board;
      if (range !== 'all') request.range = range;
      const response = await listRankings(request);
      if (response.status !== 200) throw new Error(`榜单加载失败（HTTP ${response.status}）`);
      return response.data;
    },
  });

  /** 换榜或换范围都不保留旧页码：榜单没有页码，但保留了「回到顶部」的语义。 */
  function update(next: { board?: Board; range?: Range }) {
    const merged = new URLSearchParams(params);
    if (next.board) merged.set('board', next.board);
    if (next.range) merged.set('range', next.range);
    setParams(merged);
  }

  const items = query.data?.items ?? [];

  return (
    <section className="mx-auto w-full max-w-4xl px-4 py-8">
      <header className="mb-6">
        <h1 className="text-2xl font-semibold">作品榜</h1>
        <p className="mt-1 text-sm text-gray-500 dark:text-gray-400">
          只统计当前已上架的作品；并列按「平均分 → 评分人数 → 5 分票」，最后比较晚上架。
        </p>
      </header>

      <div className="mb-6 flex flex-wrap items-center gap-2">
        {BOARD_OPTIONS.map((option) => (
          <Button
            key={option.value}
            type="button"
            primary={board === option.value}
            aria-pressed={board === option.value}
            onClick={() => update({ board: option.value })}
          >
            {option.label}
          </Button>
        ))}
        <div className="ml-auto flex items-center gap-2 text-sm">
          <label htmlFor="rankings-range">时间范围</label>
          <Select
            id="rankings-range"
            value={range}
            options={RANGE_OPTIONS}
            onChange={(next) => update({ range: next as Range })}
          />
        </div>
      </div>

      {query.isPending ? <Skeleton lines={6} /> : null}

      {query.isError ? (
        <ErrorState
          title="榜单加载失败"
          description={query.error instanceof Error ? query.error.message : '网络异常，请稍后再试'}
          onRetry={() => void query.refetch()}
          retrying={query.isFetching}
        />
      ) : null}

      {query.data && items.length === 0 ? (
        <EmptyState
          title={RATING_BOARDS.includes(board) ? '还没有人评分' : '还没有作品'}
          description={
            RATING_BOARDS.includes(board)
              ? '这个榜只统计已经有人评分的作品，所以现在还是空的。'
              : '等作品上架之后，这里会按综合热度排序。'
          }
        />
      ) : null}

      {items.length > 0 ? (
        <ol className="list-none">
          {items.map((entry) => (
            <Row key={entry.demo.slug} entry={entry} board={board} />
          ))}
        </ol>
      ) : null}
    </section>
  );
}
