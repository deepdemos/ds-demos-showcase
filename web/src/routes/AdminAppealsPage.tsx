import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { Link, Navigate } from 'react-router';
import { Button, EmptyState, ErrorState, Skeleton, Textarea } from '@/components/ui';
import { Page } from '@/components/layout/page';
import { listAdminAppeals, reviewAdminAppeal } from '@/api';
import { useAuth } from '@/features/auth/AuthContext';

/** 申诉裁决（#463）：列表 + 通过（恢复作品）/ 驳回（维持移除）。 */
export function AdminAppealsPage() {
  const { me, isLoading } = useAuth();
  const queryClient = useQueryClient();
  const [reasons, setReasons] = useState<Record<number, string>>({});
  const [error, setError] = useState<string | null>(null);

  const list = useQuery({
    queryKey: ['admin', 'appeals'],
    queryFn: async () => {
      const response = await listAdminAppeals({ status: 'pending', pageSize: 50 });
      if (response.status === 403) throw new Error('没有审核权限');
      if (response.status !== 200) throw new Error('申诉列表加载失败（' + response.status + '）');
      return response.data;
    },
    enabled: me !== null,
  });

  const review = useMutation({
    mutationFn: async (input: { id: number; decision: 'approve' | 'reject' }) => {
      const response = await reviewAdminAppeal(input.id, {
        decision: input.decision,
        reason: (reasons[input.id] || '').trim() || undefined,
      });
      if (response.status !== 200) {
        const message =
          typeof response.data === 'object' && response.data && 'message' in response.data
            ? String((response.data as { message?: unknown }).message)
            : '裁决失败（' + response.status + '）';
        throw new Error(message);
      }
      return response.data;
    },
    onSuccess: () => {
      setError(null);
      void queryClient.invalidateQueries({ queryKey: ['admin', 'appeals'] });
    },
    onError: (err) => setError(err instanceof Error ? err.message : '裁决失败'),
  });

  if (isLoading) return <Skeleton lines={4} />;
  if (me === null) return <Navigate to="/login?return_to=/admin/appeals" replace />;

  const items = list.data?.items ?? [];

  return (
    <Page
      title="待处理申诉"
      eyebrow={
        <>
          <Link className="underline" to="/admin/inbox">
            管理
          </Link>{' '}
          · 申诉
        </>
      }
      description="通过会立刻把作品恢复为 published；驳回保持 removed。裁决说明会显示给作者。"
    >
      {list.isPending ? (
        <div className="mt-4">
          <Skeleton lines={3} />
        </div>
      ) : null}

      {list.isError ? (
        <div className="mt-4">
          <ErrorState
            title="申诉列表加载失败"
            description={list.error instanceof Error ? list.error.message : '请稍后重试。'}
            onRetry={() => void list.refetch()}
            retrying={list.isFetching}
          />
        </div>
      ) : null}

      {error ? (
        <p role="alert" className="mt-3 text-sm text-red-600">
          {error}
        </p>
      ) : null}

      {list.data && items.length === 0 ? (
        <div className="mt-4">
          <EmptyState title="没有待处理申诉" description="作者提交申诉后会出现在这里。" />
        </div>
      ) : null}

      <ul className="mt-4 list-none space-y-3 p-0">
        {items.map((item) => (
          <li
            key={item.id}
            data-appeal-id={item.id}
            className="rounded-lg border border-gray-200 p-4"
          >
            <Link className="font-medium underline" to={'/demos/' + item.demoSlug}>
              {item.demoTitle || item.demoSlug}
            </Link>
            <p className="mt-1 text-xs text-gray-500">
              {item.authorName || '（未知作者）'} ·{' '}
              {new Date(item.createdAt).toLocaleString('zh-CN', { hour12: false })}
            </p>
            <p className="mt-2 text-sm">申诉说明：{item.reason}</p>

            <label className="mt-3 block text-sm">
              <span className="text-gray-500">裁决说明（可选，给作者看）</span>
              <Textarea
                className="mt-1"
                rows={2}
                maxLength={500}
                value={reasons[item.id] || ''}
                onChange={(event) =>
                  setReasons((current) => ({ ...current, [item.id]: event.target.value }))
                }
              />
            </label>

            <div className="mt-2 flex flex-wrap gap-2">
              <Button
                type="button"
                disabled={review.isPending}
                onClick={() => review.mutate({ id: item.id, decision: 'approve' })}
              >
                通过并恢复
              </Button>
              <Button
                type="button"
                danger
                disabled={review.isPending}
                onClick={() => review.mutate({ id: item.id, decision: 'reject' })}
              >
                驳回
              </Button>
            </div>
          </li>
        ))}
      </ul>
    </Page>
  );
}
