import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { Navigate, Link } from 'react-router';
import { listDemoReviewQueue, reviewEntity } from '@/api';
import type { ReviewQueueItem } from '@/api';
import { Button } from '@/components/ui/button';
import { EmptyState } from '@/components/ui/empty-state';
import { ErrorState } from '@/components/ui/error-state';
import { Skeleton } from '@/components/ui/skeleton';
import { Textarea } from '@/components/ui/textarea';
import { Page } from '@/components/layout/page';
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
 * # 为什么审核权限不在前端判
 *
 * 谁能审由服务端说了算（`demo.review` 权限）。前端**不**读 `me.role` 去猜——
 * 一猜就会和权限表脱钩：以后给某个新角色加权限，前端还得跟着改一次。
 * 这里只做一件事：收到 403 就展示「没有审核权限」，其余交给后端。
 *
 * 发布卡片是例外：`me.permissions` 是会话接口摊平的权限键（#460），
 * 按 `demo.publish` 决定渲不渲染按钮——不给一个注定 403 的按钮。
 * 服务端的 403 仍然保留，前端这层只是体验，不是鉴权。
 *
 * # 为什么预览用新标签页打开
 *
 * 预览是第三方代码（作者上传的 HTML），放进本页的 iframe 等于把审核员
 * 留在一个可能弹窗、可能占满、可能劫持键盘的页面里做裁决。
 * 新标签页里它是个独立文档，CSP sandbox 也是那边在服务端注的。
 *
 * # 已通过待发布的家
 *
 * 「通过 ≠ 已发布」：通过之后作品从待审队列消失，但还没出现在作品库。
 * 队列端点加 `state=approved_unpublished` 之后，这一页的第二个区块读服务端——
 * 刷新页面、或者换一个审核员来发布，都还能找到那些停在 `draft` 的作品
 * （已知松点第 28 条 / #433 的前置）。
 */
export function AdminReviewPage() {
  const { me, isLoading: authLoading } = useAuth();
  const queryClient = useQueryClient();
  const [reasons, setReasons] = useState<Record<string, string>>({});

  const queue = useQuery({
    queryKey: ['admin', 'reviews', 'demo', 'pending'],
    queryFn: () => listDemoReviewQueue({ limit: 50, state: 'pending' }),
    enabled: me !== null,
  });

  // 已通过、还没发布的作品。它不再依赖「刚才是谁点的通过」：刷新、换人都在。
  const publishQueue = useQuery({
    queryKey: ['admin', 'reviews', 'demo', 'approved_unpublished'],
    queryFn: () => listDemoReviewQueue({ limit: 50, state: 'approved_unpublished' }),
    enabled: me !== null,
  });

  const decide = useMutation({
    // orval 生成的是 `{ data, status }` 联合类型，**非 2xx 也不 throw**——
    // 直接 return 会让 mutationFn 永远 resolve，`decide.isError` 变成死代码，
    // 于是 409（被别人抢先审了）/ 403（权限被撤）/ 503 全都没有任何提示。
    // 所以这里必须自己看 status，把失败翻成异常。
    mutationFn: async (input: { item: ReviewQueueItem; decision: 'approve' | 'reject' }) => {
      const res = await reviewEntity('demo', input.item.slug, {
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
    onSuccess: () => {
      // 两段队列都刷新：待审那段要移走刚裁决的条目，待发布那段要接住刚通过的。
      void queryClient.invalidateQueries({ queryKey: ['admin', 'reviews', 'demo'] });
    },
  });

  // 权限键来自会话（#460）。没有它就只能按角色字符串猜，而 IA §7.2 明令不许。
  const canPublish = me?.permissions?.includes('demo.publish') ?? false;

  if (authLoading) return <Skeleton lines={3} />;
  if (me === null) return <Navigate to="/login?return_to=/admin/reviews" replace />;

  const forbidden = queue.data?.status === 403;
  // 401 = 会话没了（过期、在别处登出、cookie 被清）。它和 403 不是同一件事：
  // 403 是「你不该看」，401 是「你是谁我不知道」——后者能自救，前者不能。
  // 以前这里只判 403，于是队列 401 时页面一片空白：既没有错误态也没有队列，
  // 而 `me` 的缓存又还没失效，连「跳登录」都不触发。
  const expired = queue.data?.status === 401;

  return (
    <Page
      title="待审作品"
      description="按提交时间正序。预览会在新标签页打开——那是作者上传的代码，不在本页里跑。"
    >
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
            className="text-sm text-neutral-600 underline hover:text-neutral-900"
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
        <p role="alert" className="text-sm text-red-600">
          {decide.error instanceof Error ? decide.error.message : '裁决失败，请重试'}
        </p>
      ) : null}

      <section className="space-y-3">
        <div className="space-y-1">
          <h2 className="text-lg font-medium">已通过 · 待发布</h2>
          <p className="text-sm text-neutral-500">
            通过 ≠ 已发布。这里列出所有已通过审核、还没发布的作品；刷新或换人发布都还在。
          </p>
        </div>
        {publishQueue.isPending ? <Skeleton lines={1} /> : null}
        {publishQueue.isError ? (
          <p role="alert" className="text-sm text-red-600">
            待发布列表加载失败，请刷新重试。
          </p>
        ) : null}
        {publishQueue.data?.status === 200 ? (
          publishQueue.data.data.items.length > 0 ? (
            canPublish ? (
              <ul className="list-none space-y-4 p-0">
                {publishQueue.data.data.items.map((item) => (
                  <PublishCard
                    key={item.slug}
                    slug={item.slug}
                    title={item.title ?? '（无标题）'}
                  />
                ))}
              </ul>
            ) : (
              <p className="text-sm text-neutral-500">
                已经通过审核，但当前账号没有发布权限；由有发布权限的审核员或管理员发布。
              </p>
            )
          ) : (
            <p className="text-sm text-neutral-500">没有待发布的作品。</p>
          )
        ) : null}
      </section>

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
    </Page>
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
    <li className="rounded-lg border border-neutral-200 p-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0 space-y-1">
          <h2 className="truncate font-medium">{item.title ?? '（无标题）'}</h2>
          <p className="truncate text-sm text-neutral-500">
            {item.authorName ?? '（未知作者）'} ·{' '}
            {new Date(item.submittedAt).toLocaleString('zh-CN')}
          </p>
          {item.description ? (
            <p className="line-clamp-2 text-sm text-neutral-600">{item.description}</p>
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
        <span className="text-neutral-500">说明（可选，驳回时建议写）</span>
        <Textarea
          className="mt-1"
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
