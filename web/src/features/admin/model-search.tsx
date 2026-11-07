import { useQuery } from '@tanstack/react-query';
import { useEffect, useState } from 'react';

import {
  Button,
  Checkbox,
  EmptyState,
  ErrorState,
  Input,
  Pagination,
  Skeleton,
} from '@/components/ui';
import { listModelCatalog, type ModelCatalogItem } from '@/api';

/**
 * 左侧：上游目录搜索。
 *
 * # 为什么有分页
 *
 * 上游实测 8931 条。只给「前 50 条」等于把「找一条模型」变成「猜搜索词」——
 * 而管理员往往只知道模型的展示名，不知道上游的 provider / external_id。
 *
 * # 为什么搜索是 300ms 防抖而不是回车才搜
 *
 * 这个框的用法是「边打边看」：输 `deepseek` 想看还剩几条。回车才搜会让每次
 * 修改都变成一次手动提交，实际用起来没人会用那个回车。
 *
 * # 为什么「含上游已弃用」默认关
 *
 * 上游弃用说的是「厂商不再维护」，与我们自己的 `models.status='deprecated'`
 *（已合并到别的模型）是两套语义。默认混在一起，会让管理员以为这两件事是一回事。
 */

const LIMIT = 50;

export interface ModelSearchProps {
  q: string;
  includeDeprecated: boolean;
  offset: number;
  selectedRef: string | null;
  onSearch: (next: { q: string; includeDeprecated: boolean }) => void;
  onOffset: (offset: number) => void;
  onSelect: (item: ModelCatalogItem) => void;
}

export function refOf(item: Pick<ModelCatalogItem, 'providerId' | 'externalId'>): string {
  return `${item.providerId}/${item.externalId}`;
}

export function ModelSearch({
  q,
  includeDeprecated,
  offset,
  selectedRef,
  onSearch,
  onOffset,
  onSelect,
}: ModelSearchProps) {
  const [draft, setDraft] = useState(q);

  // URL 变了输入框要跟着变（后退、分享链接、清空）。
  useEffect(() => setDraft(q), [q]);

  // 防抖：300ms 内连续输入只发一次请求。
  useEffect(() => {
    if (draft === q) return;
    const timer = window.setTimeout(() => onSearch({ q: draft, includeDeprecated }), 300);
    return () => window.clearTimeout(timer);
  }, [draft, q, includeDeprecated, onSearch]);

  const catalog = useQuery({
    queryKey: ['admin', 'models', 'catalog', q, includeDeprecated, offset],
    queryFn: async () => {
      const res = await listModelCatalog({
        q: q === '' ? undefined : q,
        includeDeprecated,
        limit: LIMIT,
        offset,
      });
      if (res.status !== 200) {
        throw new Error(res.status === 403 ? '没有导入权限' : `目录读取失败（HTTP ${res.status}）`);
      }
      return res.data;
    },
  });

  const pageCount = catalog.data ? Math.max(1, Math.ceil(catalog.data.total / LIMIT)) : 1;
  const page = Math.floor(offset / LIMIT) + 1;

  return (
    <section className="flex min-h-0 flex-col gap-3 rounded-xl border border-gray-200 p-4 dark:border-gray-800">
      <div className="flex flex-col gap-2">
        <Input
          aria-label="搜索上游模型"
          placeholder="按名字 / id / 厂商搜索"
          value={draft}
          maxLength={100}
          onChange={(e) => setDraft((e.target as HTMLInputElement).value)}
        />
        <Checkbox
          label="含上游已弃用"
          checked={includeDeprecated}
          indeterminate={false}
          onChange={(e) => onSearch({ q, includeDeprecated: e.target.checked })}
        />
        <p className="text-xs text-gray-500 dark:text-gray-400">
          {catalog.data ? `共 ${catalog.data.total.toLocaleString('zh-CN')} 条` : '正在读取目录…'}
        </p>
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto">
        {catalog.isError ? (
          <ErrorState
            title="目录读取失败"
            description={catalog.error instanceof Error ? catalog.error.message : undefined}
            onRetry={() => void catalog.refetch()}
            retrying={catalog.isFetching}
          />
        ) : catalog.isPending ? (
          <Skeleton lines={6} lineHeight="h-10" />
        ) : catalog.data.items.length === 0 ? (
          <EmptyState
            title="没有匹配的条目"
            description={
              catalog.data.total === 0
                ? '镜像里一条都没有。先在上面点「立即同步」，把上游目录拉下来。'
                : '换个搜索词，或者勾上「含上游已弃用」。'
            }
          />
        ) : (
          <ul className="flex flex-col divide-y divide-gray-100 dark:divide-gray-800">
            {catalog.data.items.map((item) => {
              const ref = refOf(item);
              const active = ref === selectedRef;
              const chip = item.imported
                ? {
                    text: '已导入',
                    cls: 'bg-green-50 text-green-700 dark:bg-green-950 dark:text-green-300',
                  }
                : {
                    text: '可导入',
                    cls: 'bg-gray-100 text-gray-600 dark:bg-gray-800 dark:text-gray-300',
                  };
              return (
                <li key={ref}>
                  {/* 用控件库的 Button 而不是原生 button：页面与页面级组件
                      一律走控件库（AGENTS「控件库是页面可用的唯一控件来源」），
                      ui-contract 也按这条扫。 */}
                  <Button
                    onClick={() => onSelect(item)}
                    aria-current={active ? 'true' : undefined}
                    className={[
                      'w-full !justify-start px-2 py-2 text-left font-normal',
                      active
                        ? 'bg-blue-50 dark:bg-blue-950'
                        : 'hover:bg-gray-50 dark:hover:bg-gray-900',
                    ].join(' ')}
                  >
                    <span className="flex items-center gap-2">
                      <span className="truncate text-sm text-gray-900 dark:text-gray-100">
                        {item.name}
                      </span>
                      <span className={`shrink-0 rounded px-1.5 py-0.5 text-[11px] ${chip.cls}`}>
                        {chip.text}
                      </span>
                      {item.upstreamStatus === 'deprecated' ? (
                        <span className="shrink-0 rounded bg-amber-50 px-1.5 py-0.5 text-[11px] text-amber-700 dark:bg-amber-950 dark:text-amber-300">
                          上游弃用
                        </span>
                      ) : null}
                    </span>
                    <span className="mt-0.5 block truncate text-xs text-gray-500 dark:text-gray-400">
                      {item.providerName} · {item.externalId}
                    </span>
                  </Button>
                </li>
              );
            })}
          </ul>
        )}
      </div>

      <Pagination
        page={page}
        pageCount={pageCount}
        disabled={catalog.isFetching}
        onPageChange={(next) => onOffset((next - 1) * LIMIT)}
      />
    </section>
  );
}
