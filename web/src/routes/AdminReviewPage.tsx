import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { Navigate, Link } from 'react-router';
import { listDemoReviewQueue, reviewDemo } from '@/api';
import type { ReviewQueueItem } from '@/api';
import { Button } from '@/components/ui/button';
import { EmptyState } from '@/components/ui/empty-state';
import { ErrorState } from '@/components/ui/error-state';
import { Skeleton } from '@/components/ui/skeleton';
import { PublishCard } from '@/features/admin/publish-card';
import { useAuth } from '@/features/auth/AuthContext';

/**
 * 裁决失败时的兜底文案。优先用后端返回的 message（它是中文、且带具体原因），
 * 只有在响应体没有 message 时才落到这张表。
 */
const DECIDE_ERROR: Record<number, string> = {
  401: '登录已过期，请重新登录',
  403: '没有审核权限',
  409: '这条作品的状态刚刚变过，刷新后再看一遍',
  503: '服务暂时不可用，请稍后重试',
};

/** 裁决说明的长度上限，与后端 `reviewReasonMax = 500` 同口径。 */
const REASON_MAX = 500;

/**
 * 审核队列。
 *
 * # 为什么权限不在前端判
 *
 * 谁能审由服务端说了算（`demo.review` 权限）。前端**不**读 `me.role` 去猜——
 * 一猜就会和权限表脱钩：以后给某个新角色加权限，前端还得跟着改一次。
 * 这里只做一件事：收到 403 就展示「没有审核权限」，其余交给后端。
 *
 * # 为什么预览用新标签页打开
 *
 * 预览是第三方代码（作者上传的 HTML），放进本页的 iframe 等于把审核员
 * 留在一个可能弹窗、可能占满、可能劫持键盘的页面里做裁决。
 * 新标签页里它是个独立文档，CSP sandbox 也是那边在服务端注的。
 *
 * # 已知限制：通过之后没当场发布，就找不回来了
 *
 * `GET /admin/reviews/demo` 只列待审作品，没有「已通过待发布」列表端点。所以这一页
 * 的发布卡片只活在本次会话里，刷新就消失，那条作品会停在 `draft` 且界面上无处可寻。
 * 要彻底解决需要一个「已通过待发布」列表（后端 + 契约），见 `docs/已知松点.md`。
 */
export function AdminReviewPage() {
  const { me, isLoading: authLoading } = useAuth();
  const queryClient = useQueryClient();
  const [reasons, setReasons] = useState<Record<string, string>>({});
  // 本次会话里刚通过、还没发布的作品。
  // 队列只列待审，一条作品通过后就从 queue 里消失了；不留这一份，管理员点完
  // 「通过」就再也找不到它，作品会永远停在 draft。刷新页面会丢——见文件末尾注释。
  const [approved, setApproved] = useState<ReviewQueueItem[]>([]);

  const queue = useQuery({
    queryKey: ['admin', 'reviews', 'demo'],
    queryFn: () => listDemoReviewQueue({ limit: 50 }),
    enabled: me !== null,
  });

  const decide = useMutation({
    // orval 生成的是 `{ data, status }` 联合类型，**非 2xx 也不 throw**——
    // 直接 return 会让 mutationFn 永远 resolve，`decide.isError` 变成死代码，
    // 于是 409（被别人抢先审了）/ 403（权限被撤）/ 503 全都没有任何提示。
    // 所以这里必须自己看 status，把失败翻成异常。
    mutationFn: async (input: { item: ReviewQueueItem; decision: 'approve' | 'reject' }) => {
      const res = await reviewDemo(input.item.slug, {
        decision: input.decision,
        reason: reasons[input.item.slug] || undefined,
      });
      if (res.status !== 200) {
        const message =
          typeof res.data === 'object' && res.data && 'message' in res.data
            ? String((res.data as { message?: unknown }).message)
            : null;
        throw new Error(message || DECIDE_ERROR[res.status] || `裁决失败（${res.status}）`);
      }
      return res;
    },
    onSuccess: (_data, variables) => {
      if (variables.decision === 'approve') {
        // 先收进「待发布」列表再刷新队列：队列一刷新这条就不在了。
        setApproved((prev) =>
          prev.some((item) => item.slug === variables.item.slug) ? prev : [...prev, variables.item],
        );
      }
      void queryClient.invalidateQueries({ queryKey: ['admin', 'reviews', 'demo'] });
    },
  });

  if (authLoading) return <Skeleton lines={3} />;
  if (me === null) return <Navigate to="/login?return_to=/admin/reviews" replace />;

  const forbidden = queue.data?.status === 403;
  // 401 = 会话没了（过期、在别处登出、cookie 被清）。它和 403 不是同一件事：
  // 403 是「你不该看」，401 是「你是谁我不知道」——后者能自救，前者不能。
  // 以前这里只判 403，于是队列 401 时页面一片空白：既没有错误态也没有队列，
  // 而 `me` 的缓存又还没失效，连「跳登录」都不触发。
  const expired = queue.data?.status === 401;

  return (
    <div className="space-y-6">
      <header className="space-y-1">
        <h1 className="text-2xl font-semibold">待审作品</h1>
        <p className="text-sm text-neutral-500 dark:text-neutral-400">
          按提交时间正序。预览会在新标签页打开——那是作者上传的代码，不在本页里跑。
        </p>
      </header>

      {queue.isPending ? <Skeleton lines={3} /> : null}

      {queue.isError || queue.data?.status === 503 ? (
        <ErrorState
          title="队列加载失败"
          description="请稍后重试"
          onRetry={() => void queue.refetch()}
        />
      ) : null}

      {expired ? (
        // 链接放在 ErrorState 下面而不是塞进它的 description：
        // description 是纯字符串（组件刻意不接任意节点，免得错误态被拼成广告牌），
        // 而这里需要一个真的能点的出口。
        <div className="space-y-1 text-center">
          {/* 给了重试：在另一个标签页登录之后，回来点一下就能接着审，不用刷新整页。
              `check:ui` 也要求控件有接线——一个只有标题和一句话的错误态会被判为占位实现。 */}
          <ErrorState
            title="登录已过期"
            description="重新登录后即可继续审核。"
            onRetry={() => void queue.refetch()}
            retrying={queue.isFetching}
          />
          <Link
            to="/login?return_to=/admin/reviews"
            className="text-sm text-neutral-600 underline hover:text-neutral-900 dark:text-neutral-300 dark:hover:text-neutral-100"
          >
            去登录
          </Link>
        </div>
      ) : null}

      {forbidden ? (
        // 给了重试：权限可能是刚在另一个标签页给的，重试比刷新整页轻。
        <ErrorState
          title="没有审核权限"
          description="需要 demo.review 权限才能查看待审队列。"
          onRetry={() => void queue.refetch()}
        />
      ) : null}

      {decide.isError ? (
        // 裁决失败不用 ErrorState：那不是「整块内容加载失败」，
        // 而是一次操作的反馈；给整页错误态会让已加载的队列看起来也没了。
        <p role="alert" className="text-sm text-red-600 dark:text-red-400">
          {decide.error instanceof Error ? decide.error.message : '裁决失败，请重试'}
        </p>
      ) : null}

      {approved.length > 0 ? (
        <section className="space-y-3">
          <div className="space-y-1">
            <h2 className="text-lg font-medium">刚通过 · 待发布</h2>
            <p className="text-sm text-neutral-500 dark:text-neutral-400">
              通过 ≠ 已发布。只有在这里发出去的作品才会出现在作品库。
            </p>
          </div>
          <ul className="list-none space-y-4 p-0">
            {approved.map((item) => (
              <PublishCard key={item.slug} slug={item.slug} title={item.title ?? '（无标题）'} />
            ))}
          </ul>
        </section>
      ) : null}

      {queue.data?.status === 200 && queue.data.data.items.length === 0 ? (
        <EmptyState title="没有待审作品" description="有新投稿时会出现在这里。" />
      ) : null}

      {queue.data?.status === 200 && queue.data.data.items.length > 0 ? (
        <ul className="list-none space-y-4 p-0">
          {queue.data.data.items.map((item) => (
            <ReviewCard
              key={item.slug}
              item={item}
              reason={reasons[item.slug] ?? ''}
              onReasonChange={(value) => setReasons((prev) => ({ ...prev, [item.slug]: value }))}
              onDecide={(decision) => decide.mutate({ item, decision })}
              busy={decide.isPending && decide.variables?.item.slug === item.slug}
            />
          ))}
        </ul>
      ) : null}
    </div>
  );
}

function ReviewCard({
  item,
  reason,
  onReasonChange,
  onDecide,
  busy,
}: {
  item: ReviewQueueItem;
  reason: string;
  onReasonChange: (value: string) => void;
  onDecide: (decision: 'approve' | 'reject') => void;
  busy: boolean;
}) {
  return (
    <li className="rounded-lg border border-neutral-200 p-4 dark:border-neutral-800">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0 space-y-1">
          <h2 className="truncate font-medium">{item.title ?? '（无标题）'}</h2>
          <p className="truncate text-sm text-neutral-500 dark:text-neutral-400">
            {item.authorName ?? '（未知作者）'} ·{' '}
            {new Date(item.submittedAt).toLocaleString('zh-CN')}
          </p>
          {item.description ? (
            <p className="line-clamp-2 text-sm text-neutral-600 dark:text-neutral-300">
              {item.description}
            </p>
          ) : null}
        </div>
        <Link
          to={item.previewPath}
          target="_blank"
          rel="noreferrer"
          className="shrink-0 text-sm underline"
        >
          预览
        </Link>
      </div>

      <label className="mt-3 block text-sm">
        <span className="text-neutral-500 dark:text-neutral-400">说明（可选，驳回时建议写）</span>
        <textarea
          className="mt-1 w-full rounded border border-neutral-300 bg-transparent p-2 text-sm dark:border-neutral-700"
          rows={2}
          maxLength={REASON_MAX}
          value={reason}
          onChange={(event) => onReasonChange(event.target.value)}
        />
      </label>

      <div className="mt-3 flex gap-2">
        <Button primary disabled={busy} onClick={() => onDecide('approve')}>
          通过
        </Button>
        <Button disabled={busy} onClick={() => onDecide('reject')}>
          驳回
        </Button>
      </div>
    </li>
  );
}
