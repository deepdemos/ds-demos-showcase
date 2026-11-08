import { useQuery } from '@tanstack/react-query';

import { EmptyState, ErrorState, Skeleton } from '@/components/ui';
import { listDemoTimeline } from '@/api';

// 作品动态：这张表记的是「这件作品经历过什么」（谁通过、驳回、发布、改稿、回滚、
// 下架、恢复）。服务端**只给作者**，所以这个区块只在 `isAuthor` 时挂载——
// 对访客连请求都不发，否则每次看别人的作品都会多一个必然 404 的请求。
//
// 文案按动作类型给中文；未知类型直接显示原始 kind 而不是「未知动作」：
// 后端加了新动作而前端还没跟上时，看到 `merge` 比看到「未知动作」有用。

const KIND_LABELS: Record<string, string> = {
  create: '创建',
  approve: '审核通过',
  reject: '审核驳回',
  publish: '发布',
  edit: '编辑',
  rollback: '回滚到旧版本',
  archive: '下架',
  restore: '重新上架',
  slug_change: '地址变更',
  merge: '合并',
};

function formatAt(iso: string): string {
  const at = new Date(iso);
  if (Number.isNaN(at.getTime())) {
    return iso;
  }
  // 到分钟即可：动态的颗粒度是「哪一天发生了什么」，秒级精度只会让列表更吵。
  return at.toLocaleString('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  });
}

export function DemoTimeline({ slug }: { slug: string }) {
  const timeline = useQuery({
    queryKey: ['demo-timeline', slug],
    queryFn: async () => {
      const response = await listDemoTimeline(slug);
      if (response.status !== 200) {
        throw new Error(`读取作品动态失败（HTTP ${response.status}）`);
      }
      return response.data;
    },
    // 动态只在动作发生时变，而动作都来自本人或管理端的操作；
    // 用户切回页面时重取一次就够，不必高频轮询。
    staleTime: 30 * 1000,
  });

  return (
    <section className="mt-6 rounded-lg border border-gray-200 p-4 dark:border-gray-800">
      <h2 className="text-base font-medium text-gray-900 dark:text-gray-100">作品动态</h2>
      <p className="mt-1 text-xs text-gray-500 dark:text-gray-500">
        只有你能看到这份记录：谁在什么时候通过了它、发布了它、改过它。
      </p>

      {timeline.isPending ? <Skeleton lines={3} /> : null}

      {timeline.isError ? (
        <ErrorState
          title="没能读到作品动态"
          description={(timeline.error as Error).message}
          onRetry={() => {
            void timeline.refetch();
          }}
          retrying={timeline.isFetching}
        />
      ) : null}

      {timeline.data && timeline.data.items.length === 0 ? (
        <EmptyState
          title="还没有动作"
          description="通过审核、发布、改稿这些动作发生之后，这里会留下记录。"
        />
      ) : null}

      {timeline.data && timeline.data.items.length > 0 ? (
        <ol className="mt-3 space-y-2">
          {timeline.data.items.map((item, index) => (
            <li
              key={`${item.kind}-${item.at}-${index}`}
              className="flex flex-wrap items-baseline gap-x-2 text-sm text-gray-700 dark:text-gray-200"
            >
              <span className="text-gray-500 dark:text-gray-400">{formatAt(item.at)}</span>
              <span className="font-medium">{KIND_LABELS[item.kind] ?? item.kind}</span>
              {item.message ? (
                <span className="text-gray-600 dark:text-gray-300">{item.message}</span>
              ) : null}
              {item.actorName ? (
                <span className="text-gray-500 dark:text-gray-400">· {item.actorName}</span>
              ) : null}
            </li>
          ))}
        </ol>
      ) : null}
    </section>
  );
}
