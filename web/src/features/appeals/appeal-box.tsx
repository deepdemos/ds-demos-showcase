import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { Link } from 'react-router';
import { Button, Skeleton, Textarea } from '@/components/ui';
import { getDemoAppeal, submitDemoAppeal } from '@/api';
import { useAuth } from '@/features/auth/AuthContext';

/**
 * removed 墓碑的申诉入口（#463）。
 *
 * 非作者由服务端回 404，这里安静地只留下「查看社区规范」；未登录给登录入口。
 * 在途申诉显示「处理中」，不给第二次提交。
 */
export function AppealBox({ slug }: { slug: string }) {
  const { me, isLoading } = useAuth();
  const queryClient = useQueryClient();
  const [reason, setReason] = useState('');
  const [error, setError] = useState<string | null>(null);

  const state = useQuery({
    queryKey: ['demo-appeal', slug],
    queryFn: async () => {
      const response = await getDemoAppeal(slug);
      if (response.status === 401 || response.status === 404) return null;
      if (response.status !== 200) throw new Error('申诉状态加载失败（' + response.status + '）');
      return response.data;
    },
    enabled: me !== null,
  });

  const submit = useMutation({
    mutationFn: async () => {
      const response = await submitDemoAppeal(slug, { reason: reason.trim() });
      if (response.status !== 201) {
        const message =
          typeof response.data === 'object' && response.data && 'message' in response.data
            ? String((response.data as { message?: unknown }).message)
            : '提交失败（' + response.status + '）';
        throw new Error(message);
      }
      return response.data;
    },
    onSuccess: () => {
      setReason('');
      setError(null);
      void queryClient.invalidateQueries({ queryKey: ['demo-appeal', slug] });
    },
    onError: (err) => setError(err instanceof Error ? err.message : '提交失败'),
  });

  return (
    <section className="mt-4 space-y-2 text-sm" aria-label="作品申诉">
      <p>
        <Link className="underline" to="/community-guidelines">
          查看社区规范
        </Link>
      </p>

      {isLoading ? <Skeleton lines={1} /> : null}

      {!isLoading && me === null ? (
        <p className="text-gray-500">
          登录后可以提交申诉。
          <Link
            className="ml-2 underline"
            to={'/login?return_to=' + encodeURIComponent('/demos/' + slug)}
          >
            去登录
          </Link>
        </p>
      ) : null}

      {me !== null && state.isPending ? <Skeleton lines={1} /> : null}

      {me !== null && state.isError ? (
        <p role="alert" className="text-red-600">
          申诉状态加载失败。
          <Button type="button" className="ml-2" onClick={() => void state.refetch()}>
            重试
          </Button>
        </p>
      ) : null}

      {error ? (
        <p role="alert" className="text-red-600">
          {error}
        </p>
      ) : null}

      {me !== null && state.data ? (
        <>
          {state.data.appeal ? (
            <div className="rounded border border-gray-200 p-3">
              {state.data.appeal.status === 'pending' ? (
                <p>
                  申诉处理中（提交于{' '}
                  {new Date(state.data.appeal.createdAt).toLocaleString('zh-CN', { hour12: false })}
                  ）
                </p>
              ) : null}
              {state.data.appeal.status === 'approved' ? <p>申诉已通过，作品已恢复。</p> : null}
              {state.data.appeal.status === 'rejected' ? (
                <p>
                  申诉被驳回。
                  {state.data.appeal.decisionReason
                    ? '说明：' + state.data.appeal.decisionReason
                    : '管理员未填写说明。'}
                </p>
              ) : null}
              <p className="mt-1 text-xs text-gray-500">申诉说明：{state.data.appeal.reason}</p>
            </div>
          ) : null}

          {state.data.canSubmit ? (
            <div className="space-y-2">
              <label className="block">
                <span className="text-gray-500">申诉说明（1–2000 字）</span>
                <Textarea
                  className="mt-1"
                  rows={3}
                  maxLength={2000}
                  value={reason}
                  onChange={(event) => setReason(event.target.value)}
                />
              </label>
              <Button
                type="button"
                disabled={submit.isPending || reason.trim() === ''}
                onClick={() => submit.mutate()}
              >
                提交申诉
              </Button>
            </div>
          ) : null}
        </>
      ) : null}
    </section>
  );
}
