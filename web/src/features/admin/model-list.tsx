import { useQuery } from '@tanstack/react-query';
import { Link } from 'react-router';

import { listAdminModels, type AdminModelRow, type Model } from '@/api';
import { EmptyState, ErrorState, Pagination, Select, Skeleton } from '@/components/ui';
import {
  isModelDelisted,
  MODEL_STATUS_LABEL,
  PROFILE_COMPLETENESS_LABEL,
  profileCompleteness,
} from '@/features/models/profile-completeness';

export const ADMIN_MODEL_PAGE_SIZE = 20;

export type AdminModelStatusFilter = '' | Model['status'];

const STATUS_OPTIONS = [
  { value: '', label: '全部状态' },
  { value: 'active', label: '可用' },
  { value: 'candidate', label: '候选' },
  { value: 'unverified', label: '未核实' },
  { value: 'deprecated', label: '已退役' },
] as const;

class AdminModelsAccessError extends Error {
  readonly status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

export interface ModelListProps {
  status: AdminModelStatusFilter;
  page: number;
  onStatusChange: (next: AdminModelStatusFilter) => void;
  onPageChange: (next: number) => void;
}

/**
 * 管理端模型列表（#596）。
 *
 * 列出全部状态的模型，展示名称与 slug、厂商、状态、档案完整度徽章与已下架标记。
 * 筛选与分页状态由父页面写入 URL，本组件只消费 props 与回调。
 */
export function ModelList({ status, page, onStatusChange, onPageChange }: ModelListProps) {
  const query = useQuery({
    queryKey: ['admin', 'models', 'list', status],
    queryFn: async (): Promise<AdminModelRow[]> => {
      const res = await listAdminModels(status === '' ? undefined : { status });
      if (res.status === 200) return res.data.items;
      if (res.status === 401) throw new AdminModelsAccessError(401, '请先登录');
      if (res.status === 403) throw new AdminModelsAccessError(403, '没有模型管理权限');
      throw new Error('模型列表加载失败（HTTP ' + res.status + '）');
    },
    retry: false,
  });

  const items = query.data ?? [];
  const pageCount = Math.max(1, Math.ceil(items.length / ADMIN_MODEL_PAGE_SIZE));
  const current = Math.min(Math.max(1, page), pageCount);
  const visible = items.slice(
    (current - 1) * ADMIN_MODEL_PAGE_SIZE,
    current * ADMIN_MODEL_PAGE_SIZE,
  );

  const accessError = query.error instanceof AdminModelsAccessError ? query.error : null;

  return (
    <section className="rounded-xl border border-gray-200 p-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h2 className="text-sm font-medium text-gray-900">模型列表</h2>
          <p className="mt-0.5 text-xs text-gray-600">
            全部状态的站内模型，含厂商、状态、档案完整度与已下架标记。
          </p>
        </div>
        <div className="flex items-center gap-2">
          <label htmlFor="admin-model-status" className="text-sm text-gray-600">
            状态
          </label>
          <Select
            id="admin-model-status"
            value={status}
            options={STATUS_OPTIONS}
            onChange={(value) => onStatusChange(value as AdminModelStatusFilter)}
          />
        </div>
      </div>

      {query.isPending ? (
        <div className="mt-4">
          <Skeleton lines={5} />
        </div>
      ) : null}

      {accessError ? (
        <div className="mt-4">
          <EmptyState
            title={accessError.status === 401 ? '请先登录' : '没有查看权限'}
            description={
              accessError.status === 401
                ? '模型列表是管理端功能，需要先登录。'
                : '当前账号缺少模型管理权限，无法读取管理端模型列表。'
            }
          />
        </div>
      ) : null}

      {query.isError && !accessError ? (
        <div className="mt-4">
          <ErrorState
            title="模型列表加载失败"
            description={query.error instanceof Error ? query.error.message : '请稍后重试。'}
            onRetry={() => void query.refetch()}
            retrying={query.isFetching}
          />
        </div>
      ) : null}

      {query.data && items.length === 0 ? (
        <div className="mt-4">
          <EmptyState
            title="暂无模型"
            description={
              status === ''
                ? '还没有登记任何模型，可从上方导入或下方手动新建。'
                : '当前状态下没有模型。'
            }
          />
        </div>
      ) : null}

      {visible.length > 0 ? (
        <>
          <ul className="mt-4 list-none divide-y divide-gray-200 border-y border-gray-200 p-0">
            {visible.map(({ model, specs }) => {
              const completeness = profileCompleteness(specs);
              const delisted = isModelDelisted(model);
              return (
                <li
                  key={model.slug}
                  className="flex flex-wrap items-center justify-between gap-2 py-3 text-sm"
                >
                  <div className="min-w-0">
                    <div className="flex flex-wrap items-center gap-2">
                      <Link
                        className="font-medium text-gray-900 underline-offset-2 hover:underline"
                        to={'/models/' + model.slug}
                      >
                        {model.name}
                      </Link>
                      <span className="text-xs text-gray-500">{model.slug}</span>
                    </div>
                    <p className="m-0 mt-0.5 text-xs text-gray-500">
                      {model.vendor || '厂商未标注'}
                      {model.status === 'deprecated' && model.mergedIntoSlug ? (
                        <>
                          {' · 已合并到 '}
                          <Link className="underline" to={'/models/' + model.mergedIntoSlug}>
                            {model.mergedIntoSlug}
                          </Link>
                        </>
                      ) : null}
                    </p>
                  </div>

                  <div className="flex flex-wrap items-center gap-2 text-xs">
                    <span className="rounded border border-gray-300 px-2 py-0.5 text-gray-700">
                      {MODEL_STATUS_LABEL[model.status]}
                    </span>
                    <span className="rounded border border-gray-300 px-2 py-0.5 text-gray-700">
                      {PROFILE_COMPLETENESS_LABEL[completeness]}
                    </span>
                    {delisted ? (
                      <span className="rounded border border-gray-400 px-2 py-0.5 text-gray-800">
                        已下架
                      </span>
                    ) : null}
                  </div>
                </li>
              );
            })}
          </ul>
          <div className="mt-3 flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
            <span className="text-xs text-gray-500">共 {items.length} 个模型</span>
            <Pagination
              page={current}
              pageCount={pageCount}
              onPageChange={onPageChange}
              disabled={query.isFetching}
            />
          </div>
        </>
      ) : null}
    </section>
  );
}
