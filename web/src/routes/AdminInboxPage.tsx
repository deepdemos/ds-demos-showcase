import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { Link, Navigate } from 'react-router';
import { Button, Checkbox, ErrorState, Skeleton } from '@/components/ui';
import { batchReviewAdminProposals, getAdminInbox, type ProposalBatchItem } from '@/api';
import { useAuth } from '@/features/auth/AuthContext';

/**
 * 聚合收件箱（#433）。
 *
 * 这一轮只做「看得到」：三组待办 + 各自计数。动作仍走 /admin/reviews（demo 专用），
 * 统一裁决入口与提案处理下一轮接入——这里不给一个会假装成功的按钮。
 */

function formatAt(iso: string): string {
  const at = new Date(iso);
  return Number.isNaN(at.getTime()) ? '时间未知' : at.toLocaleString('zh-CN', { hour12: false });
}

export function AdminInboxPage() {
  const { me, isLoading } = useAuth();
  const queryClient = useQueryClient();
  const [selected, setSelected] = useState<number[]>([]);
  const [batchResults, setBatchResults] = useState<ProposalBatchItem[] | null>(null);
  const [batchError, setBatchError] = useState<string | null>(null);

  const inbox = useQuery({
    queryKey: ['admin', 'inbox'],
    queryFn: async () => {
      const response = await getAdminInbox({ limit: 20 });
      if (response.status === 403) throw new Error('没有审核权限');
      if (response.status !== 200) throw new Error('收件箱加载失败（' + response.status + '）');
      return response.data;
    },
    enabled: me !== null,
  });

  const batch = useMutation({
    mutationFn: async (decision: 'approve' | 'reject') => {
      const response = await batchReviewAdminProposals({ ids: selected, decision });
      if (response.status !== 200) {
        const message =
          typeof response.data === 'object' && response.data && 'message' in response.data
            ? String((response.data as { message?: unknown }).message)
            : '批量裁决失败（' + response.status + '）';
        throw new Error(message);
      }
      return response.data;
    },
    onSuccess: (data) => {
      setBatchError(null);
      setBatchResults(data.results);
      setSelected([]);
      void queryClient.invalidateQueries({ queryKey: ['admin', 'inbox'] });
    },
    onError: (err) => setBatchError(err instanceof Error ? err.message : '批量裁决失败'),
  });

  if (isLoading) return <Skeleton lines={4} />;
  if (me === null) return <Navigate to="/login?return_to=/admin/inbox" replace />;

  const data = inbox.data;

  return (
    <section className="mx-auto w-full max-w-4xl px-4 py-8">
      <header className="space-y-1">
        <h1 className="text-2xl font-semibold text-gray-900 dark:text-gray-100">收件箱</h1>
        <p className="text-sm text-gray-500 dark:text-gray-400">
          待审作品、已通过待发布、待处理提案都在这里；计数不受下面列表条数限制。
        </p>
        <Link className="text-sm underline" to="/admin/reviews">
          打开作品审核
        </Link>
      </header>

      {inbox.isPending ? (
        <div className="mt-4">
          <Skeleton lines={4} />
        </div>
      ) : null}

      {inbox.isError ? (
        <div className="mt-4">
          <ErrorState
            title="收件箱加载失败"
            description={inbox.error instanceof Error ? inbox.error.message : '请稍后重试。'}
            onRetry={() => void inbox.refetch()}
            retrying={inbox.isFetching}
          />
        </div>
      ) : null}

      {data ? (
        <>
          <div className="mt-4 grid gap-3 sm:grid-cols-3">
            <a
              href="#demo-pending"
              className="rounded-lg border border-gray-200 p-4 text-inherit no-underline dark:border-gray-800"
            >
              <p className="text-sm text-gray-500 dark:text-gray-400">待审作品</p>
              <p className="text-2xl font-semibold">{data.demoPendingCount}</p>
            </a>
            <a
              href="#approved-unpublished"
              className="rounded-lg border border-gray-200 p-4 text-inherit no-underline dark:border-gray-800"
            >
              <p className="text-sm text-gray-500 dark:text-gray-400">已通过待发布</p>
              <p className="text-2xl font-semibold">{data.approvedUnpublishedCount}</p>
            </a>
            <a
              href="#proposals"
              className="rounded-lg border border-gray-200 p-4 text-inherit no-underline dark:border-gray-800"
            >
              <p className="text-sm text-gray-500 dark:text-gray-400">待处理提案</p>
              <p className="text-2xl font-semibold">{data.proposalPendingCount}</p>
            </a>
          </div>

          <section id="demo-pending" className="mt-8">
            <h2 className="text-lg font-semibold text-gray-900 dark:text-gray-100">待审作品</h2>
            {data.demoPending.length === 0 ? (
              <p className="mt-2 text-sm text-gray-500 dark:text-gray-400">没有待审作品。</p>
            ) : (
              <ul className="mt-2 list-none space-y-2 p-0">
                {data.demoPending.map((item) => (
                  <li
                    key={item.slug}
                    className="flex flex-wrap items-center justify-between gap-2 rounded border border-gray-200 p-3 dark:border-gray-800"
                  >
                    <div className="min-w-0">
                      <Link className="underline" to={'/demos/' + item.slug}>
                        {item.title || item.slug}
                      </Link>
                      <p className="text-xs text-gray-500 dark:text-gray-400">
                        {item.authorName || '（未知作者）'} · {formatAt(item.submittedAt)}
                      </p>
                    </div>
                    <Link className="text-sm underline" to="/admin/reviews">
                      去审核
                    </Link>
                  </li>
                ))}
              </ul>
            )}
          </section>

          <section id="approved-unpublished" className="mt-8">
            <h2 className="text-lg font-semibold text-gray-900 dark:text-gray-100">已通过待发布</h2>
            {data.approvedUnpublished.length === 0 ? (
              <p className="mt-2 text-sm text-gray-500 dark:text-gray-400">没有等待发布的作品。</p>
            ) : (
              <ul className="mt-2 list-none space-y-2 p-0">
                {data.approvedUnpublished.map((item) => (
                  <li
                    key={item.slug}
                    className="flex flex-wrap items-center justify-between gap-2 rounded border border-gray-200 p-3 dark:border-gray-800"
                  >
                    <div className="min-w-0">
                      <Link className="underline" to={'/demos/' + item.slug}>
                        {item.title || item.slug}
                      </Link>
                      <p className="text-xs text-gray-500 dark:text-gray-400">
                        {item.authorName || '（未知作者）'} · {formatAt(item.submittedAt)}
                      </p>
                    </div>
                    <Link className="text-sm underline" to="/admin/reviews">
                      去发布
                    </Link>
                  </li>
                ))}
              </ul>
            )}
          </section>

          <section id="proposals" className="mt-8">
            <div className="flex flex-wrap items-center justify-between gap-2">
              <h2 className="text-lg font-semibold text-gray-900 dark:text-gray-100">待处理提案</h2>
              {data.proposals.length > 0 ? (
                <span className="text-xs text-gray-500 dark:text-gray-400">
                  已选 {selected.length} 条
                </span>
              ) : null}
            </div>
            {selected.length > 0 ? (
              <div className="mt-2 flex flex-wrap items-center gap-2">
                <Button
                  type="button"
                  disabled={batch.isPending}
                  onClick={() => batch.mutate('approve')}
                >
                  批量通过
                </Button>
                <Button
                  type="button"
                  danger
                  disabled={batch.isPending}
                  onClick={() => batch.mutate('reject')}
                >
                  批量驳回
                </Button>
              </div>
            ) : null}
            {batchError ? (
              <p role="alert" className="mt-2 text-sm text-red-600 dark:text-red-400">
                {batchError}
              </p>
            ) : null}
            {batchResults ? (
              <ul className="mt-2 list-none space-y-1 p-0 text-sm">
                {batchResults.map((result) => (
                  <li key={result.id} data-ok={result.ok}>
                    提案 #{result.id}：{result.ok ? '已处理' : result.error}
                  </li>
                ))}
              </ul>
            ) : null}
            {data.proposals.length === 0 ? (
              <p className="mt-2 text-sm text-gray-500 dark:text-gray-400">没有待处理提案。</p>
            ) : (
              <ul className="mt-2 list-none space-y-2 p-0">
                {data.proposals.map((proposal) => (
                  <li
                    key={proposal.id}
                    className="rounded border border-gray-200 p-3 dark:border-gray-800"
                  >
                    <Checkbox
                      label={'选择：' + (proposal.title || '提案 #' + proposal.id)}
                      checked={selected.includes(proposal.id)}
                      indeterminate={false}
                      disabled={batch.isPending}
                      onChange={(event) =>
                        setSelected((current) =>
                          event.target.checked
                            ? [...current, proposal.id]
                            : current.filter((id) => id !== proposal.id),
                        )
                      }
                    />
                    <p className="mt-1 text-sm">
                      <Link className="underline" to={'/admin/proposals/' + proposal.id}>
                        {proposal.title || '（没有标题）'}
                      </Link>
                      <span className="ml-2 text-xs text-gray-500 dark:text-gray-400">
                        {proposal.kind} · {proposal.source}
                      </span>
                    </p>
                    <p className="text-xs text-gray-500 dark:text-gray-400">
                      {formatAt(proposal.createdAt)}
                    </p>
                  </li>
                ))}
              </ul>
            )}
            <p className="mt-2 text-xs text-gray-500 dark:text-gray-400">
              提案审核入口下一轮接入；这一页先保证待办不会被漏看。
            </p>
          </section>
        </>
      ) : null}
    </section>
  );
}
