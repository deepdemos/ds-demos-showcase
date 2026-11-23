import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { Link, Navigate, useParams } from 'react-router';
import { Button, EmptyState, ErrorState, Skeleton, Textarea } from '@/components/ui';
import { Page } from '@/components/layout/page';
import { getAdminProposal, reviewEntity } from '@/api';
import { useAuth } from '@/features/auth/AuthContext';

/**
 * 提案详情（#433）。
 *
 * 这一轮只做「看得清 + 能驳回」：拒绝不写任何正式业务表（ENT-023）。
 * 通过需要按 kind 执行，执行器未接入前按钮是 disabled 的——不给一个注定 422 的按钮。
 */

const REASON_MAX = 500;

// 已经接入执行器的提案 kind；其余 kind 不渲染「通过」按钮，避免注定 422 的点击。
const APPROVABLE_KINDS = ['new_classification', 'new_skill', 'new_task'];

const STATUS_LABEL: Record<string, string> = {
  pending: '待处理',
  approved: '已通过',
  rejected: '已驳回',
  withdrawn: '已撤回',
  superseded: '已被合并替代',
};

function formatAt(iso: string | null | undefined): string {
  if (!iso) return '—';
  const at = new Date(iso);
  return Number.isNaN(at.getTime()) ? '时间未知' : at.toLocaleString('zh-CN', { hour12: false });
}

export function ProposalDetailPage() {
  const params = useParams();
  const id = Number(params.id);
  const { me, isLoading: authLoading } = useAuth();
  const queryClient = useQueryClient();
  const [reason, setReason] = useState('');
  const [error, setError] = useState<string | null>(null);

  const detail = useQuery({
    queryKey: ['admin', 'proposal', id],
    queryFn: async () => {
      const response = await getAdminProposal(id);
      if (response.status === 403) throw new Error('没有提案审核权限');
      if (response.status === 404) return null;
      if (response.status !== 200) throw new Error('提案加载失败（' + response.status + '）');
      return response.data;
    },
    enabled: me !== null && Number.isInteger(id) && id > 0,
  });

  const reject = useMutation({
    mutationFn: async () => {
      const response = await reviewEntity('proposal', String(id), {
        decision: 'reject',
        reason: reason.trim() || undefined,
      });
      if (response.status !== 200) {
        const message =
          typeof response.data === 'object' && response.data && 'message' in response.data
            ? String((response.data as { message?: unknown }).message)
            : '驳回失败（' + response.status + '）';
        throw new Error(message);
      }
      return response.data;
    },
    onSuccess: () => {
      setError(null);
      void queryClient.invalidateQueries({ queryKey: ['admin', 'proposal', id] });
      void queryClient.invalidateQueries({ queryKey: ['admin', 'proposals'] });
      void queryClient.invalidateQueries({ queryKey: ['admin', 'inbox'] });
    },
    onError: (err) => setError(err instanceof Error ? err.message : '驳回失败'),
  });

  const approve = useMutation({
    mutationFn: async () => {
      const response = await reviewEntity('proposal', String(id), { decision: 'approve' });
      if (response.status !== 200) {
        const message =
          typeof response.data === 'object' && response.data && 'message' in response.data
            ? String((response.data as { message?: unknown }).message)
            : '通过失败（' + response.status + '）';
        throw new Error(message);
      }
      return response.data;
    },
    onSuccess: () => {
      setError(null);
      void queryClient.invalidateQueries({ queryKey: ['admin', 'proposal', id] });
      void queryClient.invalidateQueries({ queryKey: ['admin', 'proposals'] });
      void queryClient.invalidateQueries({ queryKey: ['admin', 'inbox'] });
    },
    onError: (err) => setError(err instanceof Error ? err.message : '通过失败'),
  });

  if (authLoading) return <Skeleton lines={3} />;
  if (!Number.isInteger(id) || id < 1) {
    return (
      <Page title="提案详情">
        <EmptyState title="地址里的提案编号不合法" description="编号必须是正整数。" />
      </Page>
    );
  }
  if (me === null) return <Navigate to={'/login?return_to=/admin/proposals/' + id} replace />;

  if (detail.isPending) {
    return (
      <Page title="提案详情">
        <Skeleton lines={4} />
      </Page>
    );
  }

  if (detail.isError) {
    return (
      <Page title="提案详情">
        <ErrorState
          title="提案加载失败"
          description={detail.error instanceof Error ? detail.error.message : '请稍后重试。'}
          onRetry={() => void detail.refetch()}
          retrying={detail.isFetching}
        />
      </Page>
    );
  }

  if (detail.data === null) {
    return (
      <Page title="提案详情">
        <EmptyState title="提案不存在" description="它可能已经被擦除，或者编号不对。" />
      </Page>
    );
  }

  const data = detail.data;
  const pending = data.status === 'pending';

  return (
    <Page
      title={String(data.payload?.title ?? data.payload?.name ?? data.kind)}
      eyebrow={
        <>
          <Link className="underline" to="/admin/inbox">
            收件箱
          </Link>{' '}
          · 提案 #{data.id}
        </>
      }
      description={
        data.kind +
        ' · ' +
        data.source +
        ' · ' +
        (STATUS_LABEL[data.status] ?? data.status) +
        ' · 提交于 ' +
        formatAt(data.createdAt)
      }
    >
      <h2 className="mt-6 text-lg font-semibold text-gray-900">内容</h2>
      <pre className="mt-2 overflow-x-auto rounded border border-gray-200 bg-gray-50 p-3 text-xs">
        {JSON.stringify(data.payload ?? {}, null, 2)}
      </pre>

      {data.status === 'rejected' ? (
        <p className="mt-4 text-sm text-gray-700">
          驳回原因：{data.rejectReason || '管理员未填写理由'}
        </p>
      ) : null}

      {pending ? (
        <section className="mt-6 space-y-3 rounded-lg border border-gray-200 p-4">
          <h2 className="text-lg font-semibold text-gray-900">裁决</h2>
          <label className="block text-sm">
            <span className="text-gray-500">驳回原因（可选）</span>
            <Textarea
              className="mt-1"
              rows={2}
              maxLength={REASON_MAX}
              value={reason}
              onChange={(event) => setReason(event.target.value)}
            />
          </label>
          <div className="flex flex-wrap items-center gap-2">
            <Button
              type="button"
              danger
              disabled={reject.isPending}
              onClick={() => reject.mutate()}
            >
              驳回
            </Button>
            {APPROVABLE_KINDS.includes(data.kind) ? (
              <Button
                type="button"
                primary
                disabled={approve.isPending}
                onClick={() => approve.mutate()}
              >
                通过
              </Button>
            ) : (
              <span className="text-sm text-gray-500">通过（执行器未接入）</span>
            )}
          </div>
          <p className="text-xs text-gray-500">
            {APPROVABLE_KINDS.includes(data.kind)
              ? '通过会写对应的正式表（已存在则复用），并把提案置为已通过。'
              : '这个 kind 的执行器还没接入；驳回只改提案状态并写审计。'}
          </p>
          {error ? (
            <p role="alert" className="text-sm text-red-600">
              {error}
            </p>
          ) : null}
        </section>
      ) : null}
    </Page>
  );
}
