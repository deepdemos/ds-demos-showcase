import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { Link, Navigate } from 'react-router';
import { Button, EmptyState, ErrorState, Pagination, Skeleton } from '@/components/ui';
import { Page } from '@/components/layout/page';
import {
  listNotifications,
  markAllNotificationsRead,
  markNotificationRead,
  type Notification,
} from '@/api';
import { useAuth } from '@/features/auth/AuthContext';

/**
 * 通知列表（#437 / NTF-001、NTF-006、NTF-007；#591 起对象泛化）。
 *
 * 文案由前端按 kind 组装：服务端只给事件类型 + payload，改文案不用回填历史数据（NTF-003）。
 * 被通知的对象是「类型 + 键 + 标题快照」（objectType / objectKey / objectTitle），
 * 所以作品、模型、提案都能挂在这里；对象不可访问时不给跳转，只显示标题与说明（NTF-007）。
 *
 * 旧的 demoSlug / demoTitle 还在响应里（过渡期展示缓存），对象字段为空时用它兜底。
 */

const PAGE_SIZE = 20;

/** 不可访问时要说清是「作品」「模型」还是泛指的「对象」。 */
const OBJECT_NOUN: Record<string, string> = { demo: '作品', model: '模型' };

function objectNoun(item: Notification): string {
  return OBJECT_NOUN[item.objectType] ?? '对象';
}

/** 对象入口：只有真的有页面可去的类型才给链接（提案的入口由 #595 另行决定）。 */
function objectLink(item: Notification): { href: string; label: string } | null {
  switch (item.objectType) {
    case 'demo':
      return { href: '/demos/' + item.objectKey, label: '查看作品' };
    case 'model':
      return { href: '/models/' + item.objectKey, label: '查看模型' };
    default:
      return null;
  }
}

function notificationTitle(item: Notification): string {
  // 对象快照优先；旧字段只为作品类事件填充，也允许为空——两层兜底，别渲染出空书名号。
  const title = item.objectTitle || item.objectKey || item.demoTitle || item.demoSlug;
  switch (item.kind) {
    case 'review.approved':
      return '《' + title + '》已通过审核';
    case 'review.rejected':
      return '《' + title + '》被驳回';
    case 'rating.received': {
      const score = Number(item.payload.score);
      return Number.isFinite(score)
        ? '有人给《' + title + '》打了 ' + score + ' 分'
        : '有人给《' + title + '》打了分';
    }
    case 'model.proposal.approved':
      return '《' + title + '》的收录提案已通过';
    case 'model.proposal.rejected':
      return '《' + title + '》的收录提案被驳回';
    default:
      return '关于《' + title + '》的通知';
  }
}

function notificationReason(item: Notification): string | null {
  const reason = item.payload.reason;
  return typeof reason === 'string' && reason.trim() !== '' ? reason : null;
}

function formatAt(iso: string): string {
  const at = new Date(iso);
  return Number.isNaN(at.getTime()) ? '时间未知' : at.toLocaleString('zh-CN', { hour12: false });
}

export function NotificationsPage() {
  const { me, isLoading } = useAuth();
  const queryClient = useQueryClient();
  const [page, setPage] = useState(1);
  const [error, setError] = useState<string | null>(null);

  const list = useQuery({
    queryKey: ['notifications', page],
    queryFn: async () => {
      const response = await listNotifications({ page, pageSize: PAGE_SIZE });
      if (response.status !== 200) throw new Error('通知加载失败（' + response.status + '）');
      return response.data;
    },
    enabled: me !== null,
  });

  const invalidate = () => {
    void queryClient.invalidateQueries({ queryKey: ['notifications'] });
    void queryClient.invalidateQueries({ queryKey: ['notifications', 'unread'] });
  };

  const markRead = useMutation({
    mutationFn: async (id: number) => {
      const response = await markNotificationRead(id);
      if (response.status !== 204) throw new Error('标记已读失败（' + response.status + '）');
    },
    onSuccess: () => {
      setError(null);
      invalidate();
    },
    onError: (err) => setError(err instanceof Error ? err.message : '标记已读失败'),
  });

  const markAll = useMutation({
    mutationFn: async () => {
      const response = await markAllNotificationsRead();
      if (response.status !== 204) throw new Error('全部已读失败（' + response.status + '）');
    },
    onSuccess: () => {
      setError(null);
      invalidate();
    },
    onError: (err) => setError(err instanceof Error ? err.message : '全部已读失败'),
  });

  if (isLoading) return <Skeleton lines={4} />;
  if (me === null) return <Navigate to="/login?return_to=/me/notifications" replace />;

  const items = list.data?.items ?? [];
  const unreadCount = list.data?.unreadCount ?? 0;
  const pageCount = Math.max(1, Math.ceil((list.data?.total ?? 0) / PAGE_SIZE));

  return (
    <Page
      title="通知"
      description="审核结果与收到评分会出现在这里；已读之后仍可回看。"
      actions={
        <Button
          type="button"
          disabled={unreadCount === 0 || markAll.isPending}
          onClick={() => markAll.mutate()}
        >
          全部已读
        </Button>
      }
    >
      {error ? (
        <p role="alert" className="mt-3 text-sm text-red-600">
          {error}
        </p>
      ) : null}

      {list.isError ? (
        <div className="mt-4">
          <ErrorState
            title="通知加载失败"
            description="请稍后重试。"
            onRetry={() => void list.refetch()}
            retrying={list.isFetching}
          />
        </div>
      ) : null}

      {list.isPending ? (
        <div className="mt-4">
          <Skeleton lines={3} />
        </div>
      ) : null}

      {list.data && items.length === 0 ? (
        <div className="mt-4">
          <EmptyState
            title="还没有通知"
            description="作品审核有结果、或有人给你的作品打分时，这里会收到提醒。"
          />
        </div>
      ) : null}

      {items.length > 0 ? (
        <ul className="mt-4 list-none space-y-3 p-0">
          {items.map((item) => {
            const reason = notificationReason(item);
            const link = item.accessible ? objectLink(item) : null;
            return (
              <li
                key={item.id}
                data-kind={item.kind}
                data-read={item.readAt !== null}
                className={
                  'rounded-lg border p-4 ' +
                  (item.readAt === null ? 'border-gray-300 bg-gray-50' : 'border-gray-200')
                }
              >
                <div className="flex flex-wrap items-start justify-between gap-2">
                  <div className="min-w-0">
                    <p className="text-sm font-medium text-gray-900">
                      {item.readAt === null ? (
                        <span className="mr-2 rounded bg-gray-200 px-1 text-xs text-gray-700">
                          未读
                        </span>
                      ) : null}
                      {notificationTitle(item)}
                    </p>
                    <p className="mt-1 text-xs text-gray-500">{formatAt(item.createdAt)}</p>
                    {reason ? <p className="mt-2 text-sm text-gray-700">理由：{reason}</p> : null}
                    {link ? (
                      <Link className="mt-2 inline-block text-sm underline" to={link.href}>
                        {link.label}
                      </Link>
                    ) : item.accessible ? null : (
                      <p className="mt-2 text-sm text-gray-500">该{objectNoun(item)}已不可访问</p>
                    )}
                  </div>
                  {item.readAt === null ? (
                    <Button
                      type="button"
                      aria-label={'标为已读 ' + notificationTitle(item)}
                      disabled={markRead.isPending}
                      onClick={() => markRead.mutate(item.id)}
                    >
                      标为已读
                    </Button>
                  ) : null}
                </div>
              </li>
            );
          })}
        </ul>
      ) : null}

      {items.length > 0 ? (
        <Pagination page={page} pageCount={pageCount} onPageChange={setPage} />
      ) : null}
    </Page>
  );
}
