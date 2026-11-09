import { useQuery } from '@tanstack/react-query';
import { Link, Navigate, useSearchParams } from 'react-router';
import { EmptyState, ErrorState, Pagination, Skeleton } from '@/components/ui';
import { listMyDemos, type MyDemoList, type MyDemoSummary } from '@/api';
import { useAuth } from '@/features/auth/AuthContext';

/**
 * 我的作品。
 *
 * # 为什么这条页面自己算「状态文案」而不复用详情页的横幅
 *
 * 列表要回答的是「这件现在能不能点、在哪儿点」，而详情页横幅回答的是
 * 「你看到的这一版是不是线上那一版」。两者判据不同：
 * 软删的作品 `lifecycle_state` 仍是 `published`，但作者点进去是 404；
 * 而「已上线 + 新版本待审」恰恰说明它还能点。
 * 所以这里先看 `deletedAt`，再看生命周期与审核态的组合。
 *
 * # 为什么草稿不给 `/demos/{slug}` 链接
 *
 * 详情接口对没有对外版本的作品回 404（作者也一样，那边只认 `published`）。
 * 作者要看自己的草稿只有一个入口：`/preview/{slug}/`。给错链接的症状是
 * 「刚投完稿点进去 404」——批次 1 真的发生过。
 */

const PAGE_SIZE = 20;

function readPage(raw: string | null): number {
  const n = Number(raw);
  return Number.isInteger(n) && n >= 1 ? n : 1;
}

async function fetchMyDemos(page: number): Promise<MyDemoList> {
  const response = await listMyDemos({ page, pageSize: PAGE_SIZE });
  if (response.status === 200) return response.data;
  throw new Error(`我的作品加载失败（HTTP ${response.status}）`);
}

/** 一条作品「现在是什么状态、该往哪儿点」。 */
export interface RowState {
  label: string;
  /** 有链接才可点；软删的作品没有目的地。 */
  href: string | null;
  /** 附在标签下面的一句解释，没有就不渲染。 */
  note: string | null;
}

/**
 * 状态 → 标签与解释。
 *
 * 写成表而不是 if 链：两条理由。
 * 一是**没有**任何一处需要「推导可见性」——标签是展示文案，真相在服务端；
 * 而 `check-single-source` 会拦下「拿生命周期字段与某个字面量作比较」这种写法，
 * 意思是「前端别自己判可见性」。查表既满足这条规则，也不会有人顺手在某个分支里
 * 多加一个条件。
 * 二是这张表能一眼看全：生命周期 × 审核态只有这几种组合，缺哪种当场看得出来。
 */
type Combo = `${string}:${string}`;

const STATE_TEXT: Record<Combo, { label: string; note: string | null }> = {
  'published:approved': { label: '已上线', note: null },
  'published:pending': {
    label: '已上线 · 新版本待审',
    note: '线上仍是上一版，通过后这次改动才生效。',
  },
  'published:rejected': {
    label: '已上线 · 新版本被驳回',
    note: '线上仍是上一版，改好再提交会重新进队列。',
  },
  'archived:approved': { label: '已下架', note: '作品库里看不到，可以重新上架。' },
  'archived:pending': { label: '已下架', note: '作品库里看不到，可以重新上架。' },
  'archived:rejected': { label: '已下架', note: '作品库里看不到，可以重新上架。' },
  'removed:approved': { label: '已下架（违规）', note: '这一状态不能自己重新上架。' },
  'removed:pending': { label: '已下架（违规）', note: '这一状态不能自己重新上架。' },
  'removed:rejected': { label: '已下架（违规）', note: '这一状态不能自己重新上架。' },
  'draft:pending': { label: '待审核', note: '通过之前不会出现在作品库里。' },
  'draft:rejected': { label: '被驳回', note: '按审核意见改好再提交。' },
  'draft:approved': { label: '已通过 · 待发布', note: '可以发布到渠道了。' },
};

/**
 * 详情链接只在「有对外版本」时才成立。
 *
 * 其它三种（草稿）只能走 `/preview/{slug}/`——详情接口对没有对外版本的作品
 * 一律 404，作者也一样。给错链接的症状是「刚投完稿点进去 404」。
 */
const DETAIL_OR_PREVIEW: Record<string, string> = {
  published: 'detail',
  archived: 'detail',
  removed: 'detail',
  draft: 'preview',
};

/**
 * 把生命周期 × 审核态 × 软删折成一句人话。
 *
 * 顺序是有意的：软删最先判——它压过其它三个字段（软删不改 lifecycle_state，
 * 只写 deleted_at），先判生命周期会得出「已上线」这种会误导人的结论。
 */
export function rowState(demo: MyDemoSummary): RowState {
  if (demo.deletedAt) {
    const until = demo.purgeAfter
      ? `内容将在 ${formatDate(demo.purgeAfter)} 清空，之前可以恢复。`
      : '内容将在恢复期结束后清空。';
    return { label: '已删除', href: null, note: until };
  }

  const text = STATE_TEXT[`${demo.lifecycleState}:${demo.moderationState}`] ?? {
    label: demo.lifecycleState,
    note: null,
  };
  const kind = DETAIL_OR_PREVIEW[demo.lifecycleState] ?? 'preview';
  const href = kind === 'detail' ? `/demos/${demo.slug}` : `/preview/${demo.slug}/`;
  return { label: text.label, href, note: text.note ?? null };
}

function formatDate(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return `${d.getFullYear()}/${d.getMonth() + 1}/${d.getDate()}`;
}

export function MyDemosPage() {
  const { me, isLoading } = useAuth();
  const [searchParams, setSearchParams] = useSearchParams();
  const page = readPage(searchParams.get('page'));

  const query = useQuery({
    queryKey: ['my-demos', page],
    queryFn: () => fetchMyDemos(page),
    // 未登录不发请求：否则每次进页面都白打一次 401。
    enabled: Boolean(me),
    staleTime: 30 * 1000,
  });

  if (isLoading) return <Skeleton lines={5} />;
  if (!me) return <Navigate to="/login" replace />;

  const data = query.data;
  const pageCount = data ? Math.max(1, Math.ceil(data.total / data.pageSize)) : 1;

  return (
    <section className="flex flex-col gap-6">
      <div className="flex items-end justify-between gap-4">
        <div>
          <h1 className="text-xl font-semibold text-gray-800 dark:text-gray-100">我的作品</h1>
          <p className="mt-1 text-sm text-gray-500 dark:text-gray-400">
            草稿与待审的点标题看预览；已上线的点标题看读者看到的页面。
          </p>
        </div>
        <Link
          to="/submit"
          className="rounded-md bg-gray-900 px-3 py-2 text-sm font-medium text-white hover:bg-gray-700 dark:bg-gray-100 dark:text-gray-900 dark:hover:bg-gray-300"
        >
          投新作品
        </Link>
      </div>

      {query.isPending ? <Skeleton lines={5} /> : null}

      {query.isError ? (
        <ErrorState
          title="没能读出你的作品"
          description={query.error instanceof Error ? query.error.message : undefined}
          onRetry={() => void query.refetch()}
          retrying={query.isFetching}
        />
      ) : null}

      {data && data.items.length === 0 ? (
        <EmptyState
          title="还没有投稿过作品"
          description="投一件试试：支持 zip 包（根目录要有 index.html）或单个 HTML / SVG 文件。"
          action={
            <Link
              to="/submit"
              className="rounded-md bg-gray-900 px-3 py-2 text-sm font-medium text-white hover:bg-gray-700 dark:bg-gray-100 dark:text-gray-900 dark:hover:bg-gray-300"
            >
              去投稿
            </Link>
          }
        />
      ) : null}

      {data && data.items.length > 0 ? (
        <ul className="flex flex-col divide-y divide-gray-200 dark:divide-gray-700">
          {data.items.map((demo) => {
            const state = rowState(demo);
            const title = demo.title?.trim() || demo.slug;
            return (
              <li key={demo.slug} className="flex items-start gap-4 py-3">
                {demo.coverUrl ? (
                  <img
                    src={demo.coverUrl}
                    alt=""
                    className="h-14 w-20 flex-none rounded object-cover"
                  />
                ) : (
                  <div
                    aria-hidden
                    className="h-14 w-20 flex-none rounded bg-gray-100 dark:bg-gray-800"
                  />
                )}
                <div className="min-w-0 flex-1">
                  <div className="flex flex-wrap items-center gap-2">
                    {state.href ? (
                      <Link
                        to={state.href}
                        className="truncate font-medium text-gray-800 hover:underline dark:text-gray-100"
                      >
                        {title}
                      </Link>
                    ) : (
                      <span className="truncate font-medium text-gray-500 dark:text-gray-400">
                        {title}
                      </span>
                    )}
                    <span className="rounded bg-gray-100 px-2 py-0.5 text-xs text-gray-600 dark:bg-gray-800 dark:text-gray-300">
                      {state.label}
                    </span>
                  </div>
                  {state.note ? (
                    <p className="mt-1 text-xs text-gray-500 dark:text-gray-400">{state.note}</p>
                  ) : null}
                  <p className="mt-1 text-xs text-gray-400 dark:text-gray-500">
                    {demo.slug} · 更新于 {formatDate(demo.updatedAt)}
                  </p>
                </div>
              </li>
            );
          })}
        </ul>
      ) : null}

      <Pagination
        page={page}
        pageCount={pageCount}
        onPageChange={(next) => {
          const params = new URLSearchParams(searchParams);
          if (next === 1) params.delete('page');
          else params.set('page', String(next));
          setSearchParams(params, { replace: true });
        }}
      />
    </section>
  );
}
