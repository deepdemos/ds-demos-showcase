import { Link, useSearchParams } from 'react-router';
import { useQuery } from '@tanstack/react-query';
import { EmptyState, ErrorState, Pagination, Select, Skeleton } from '@/components/ui';
import { Page } from '@/components/layout/page';
import { listAdminProposals, type ListAdminProposalsStatus, type ProposalList } from '@/api';

const PAGE_SIZE = 20;
const STATUS_OPTIONS: Array<{ value: ListAdminProposalsStatus | 'all'; label: string }> = [
  { value: 'pending', label: '待处理' },
  { value: 'approved', label: '已通过' },
  { value: 'rejected', label: '已驳回' },
  { value: 'withdrawn', label: '已撤回' },
  { value: 'superseded', label: '已被取代' },
  { value: 'all', label: '全部' },
];

const STATUS_LABEL: Record<string, string> = {
  pending: '待处理',
  approved: '已通过',
  rejected: '已驳回',
  withdrawn: '已撤回',
  superseded: '已被取代',
};

export function ProposalsPage() {
  const [params, setParams] = useSearchParams();
  const status = (params.get('status') ?? 'pending') as ListAdminProposalsStatus | 'all';
  const page = Math.max(1, Number(params.get('page')) || 1);

  const query = useQuery({
    queryKey: ['admin-proposals', status, page],
    queryFn: async (): Promise<ProposalList> => {
      const response = await listAdminProposals({
        status: status === 'all' ? undefined : status,
        page,
        pageSize: PAGE_SIZE,
      });
      if (response.status !== 200) throw new Error('提案列表加载失败（' + response.status + '）');
      return response.data;
    },
  });

  function goToPage(nextPage: number) {
    const next = new URLSearchParams(params);
    if (nextPage <= 1) next.delete('page');
    else next.set('page', String(nextPage));
    setParams(next, { replace: true });
  }

  return (
    <Page
      title="提案"
      description="来自用户的实体提案；点标题查看 payload 并裁决。"
      actions={
        <Link className="text-sm underline" to="/admin/inbox">
          回收件箱
        </Link>
      }
    >
      <div className="mt-6 flex items-center gap-2">
        <label htmlFor="proposal-status" className="text-sm text-gray-600">
          状态
        </label>
        <Select
          id="proposal-status"
          value={status}
          options={STATUS_OPTIONS.map((option) => ({ ...option }))}
          onChange={(next) => {
            const params = new URLSearchParams();
            if (next !== 'pending') params.set('status', next);
            setParams(params, { replace: true });
          }}
        />
      </div>

      {query.isPending ? (
        <div className="mt-4">
          <Skeleton lines={5} />
        </div>
      ) : null}

      {query.isError ? (
        <div className="mt-4">
          <ErrorState
            title="提案列表加载失败"
            description={query.error instanceof Error ? query.error.message : '请稍后重试。'}
            onRetry={() => void query.refetch()}
            retrying={query.isFetching}
          />
        </div>
      ) : null}

      {query.data && query.data.items.length === 0 ? (
        <div className="mt-4">
          <EmptyState title="没有提案" description="换一个状态看看，或者等新的提案进来。" />
        </div>
      ) : null}

      {query.data && query.data.items.length > 0 ? (
        <>
          <ul className="mt-4 list-none divide-y divide-gray-200 border-y border-gray-200 p-0">
            {query.data.items.map((proposal) => (
              <li
                key={proposal.id}
                className="flex flex-wrap items-center justify-between gap-2 py-3"
              >
                <div className="min-w-0">
                  <Link
                    className="font-medium underline-offset-2 hover:underline"
                    to={'/admin/proposals/' + proposal.id}
                  >
                    {proposal.title || '提案 #' + proposal.id}
                  </Link>
                  <p className="m-0 mt-1 text-xs text-gray-500">
                    #{proposal.id} · {proposal.kind} · {proposal.source} ·{' '}
                    {STATUS_LABEL[proposal.status] ?? proposal.status}
                  </p>
                </div>
                <span className="text-xs text-gray-500">
                  {new Date(proposal.createdAt).toLocaleDateString('zh-CN')}
                </span>
              </li>
            ))}
          </ul>
          <div className="mt-4 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
            <span className="text-sm text-gray-500">共 {query.data.total} 条提案</span>
            <Pagination
              page={query.data.page}
              pageCount={Math.max(1, Math.ceil(query.data.total / query.data.pageSize))}
              onPageChange={goToPage}
              disabled={query.isFetching}
            />
          </div>
        </>
      ) : null}
    </Page>
  );
}
