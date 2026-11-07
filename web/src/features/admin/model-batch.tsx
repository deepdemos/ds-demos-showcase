import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';

import { Button, EmptyState } from '@/components/ui';
import { applyModelImportBatch, type ModelImportBatchResult } from '@/api';

import type { SelectedPayload } from './model-diff';

/**
 * 底部：待导入清单 + 批量落库。
 *
 * # 为什么批量是「先逐条勾字段，再一起提交」
 *
 * 导入写的是**共享的模型目录**：一条改错会影响所有引用该模型的版本。所以每一条
 * 都要先看过差异、勾过字段，再一起提。批量在这里省的是「重复点击提交」，
 * 不是「重复判断」。
 *
 * # 为什么逐条报 created / completed / failed
 *
 * 服务端是逐条事务（某条失败不连坐）。如果界面只报「已导入 3 条」，管理员没法
 * 知道失败的到底是哪条、为什么——而那正是他最需要去补的信息。
 */

/** 与服务端 `maxBatchImportItems` 一致。超了按钮就该禁用，不必让用户等一次 400。 */
export const BATCH_LIMIT = 50;

export interface ModelBatchProps {
  queue: SelectedPayload[];
  onRemove: (index: number) => void;
  onClear: () => void;
}

export function ModelBatch({ queue, onRemove, onClear }: ModelBatchProps) {
  const queryClient = useQueryClient();
  const [results, setResults] = useState<ModelImportBatchResult['results'] | undefined>();

  const apply = useMutation({
    mutationFn: async () => {
      const res = await applyModelImportBatch({
        items: queue.map((q) => ({
          source: 'models.dev',
          providerId: q.ref.providerId,
          externalId: q.ref.externalId,
          ...(q.label ? { name: q.label } : {}),
          fields: q.fields,
        })),
      });
      if (res.status !== 200) throw new Error(`批量导入失败（HTTP ${res.status}）`);
      return res.data;
    },
    onSuccess: (data) => {
      setResults(data.results);
      // 成功的那些已经变成「已导入」，目录与差异都要重取——否则左边徽标还是旧值。
      void queryClient.invalidateQueries({ queryKey: ['admin', 'models'] });
    },
  });

  if (queue.length === 0 && !results) {
    return (
      <section className="rounded-xl border border-dashed border-gray-200 p-4 dark:border-gray-800">
        <EmptyState
          title="待导入清单还是空的"
          description="在右侧勾好字段后点「加入待导入」，可以攒多条一起提交。"
        />
      </section>
    );
  }

  const okCount = results?.filter((r) => r.status !== 'failed').length ?? 0;
  const failed = results?.filter((r) => r.status === 'failed') ?? [];

  return (
    <section className="rounded-xl border border-gray-200 p-4 dark:border-gray-800">
      <div className="flex flex-wrap items-center gap-3">
        <h2 className="text-sm font-medium text-gray-900 dark:text-gray-100">
          待导入 {queue.length} 条
        </h2>
        {queue.length > BATCH_LIMIT ? (
          <span role="alert" className="text-xs text-red-700 dark:text-red-400">
            一次最多 {BATCH_LIMIT} 条，先提交一批
          </span>
        ) : null}
        <span className="ml-auto" />
        <Button
          onClick={() => apply.mutate()}
          disabled={apply.isPending || queue.length === 0 || queue.length > BATCH_LIMIT}
        >
          {apply.isPending ? '导入中…' : '批量导入'}
        </Button>
        <Button disabled={apply.isPending || queue.length === 0} onClick={onClear}>
          清空
        </Button>
      </div>

      {queue.length > 0 ? (
        <ul className="mt-3 flex flex-col divide-y divide-gray-100 dark:divide-gray-800">
          {queue.map((q, i) => (
            <li
              key={`${q.ref.providerId}/${q.ref.externalId}`}
              className="flex items-center gap-3 py-2"
            >
              <span className="truncate text-sm text-gray-900 dark:text-gray-100">{q.label}</span>
              <span className="truncate text-xs text-gray-500 dark:text-gray-400">
                {q.fields.length} 个字段
              </span>
              <span className="ml-auto" />
              <Button onClick={() => onRemove(i)} disabled={apply.isPending}>
                移除
              </Button>
            </li>
          ))}
        </ul>
      ) : null}

      {apply.isError ? (
        <p role="alert" className="mt-3 text-xs text-red-700 dark:text-red-400">
          {apply.error instanceof Error ? apply.error.message : '批量导入失败'}
        </p>
      ) : null}

      {results ? (
        <div className="mt-3">
          <p role="status" className="text-xs text-gray-700 dark:text-gray-300">
            成功 {okCount} 条{failed.length > 0 ? `，失败 ${failed.length} 条` : ''}
          </p>
          <ul className="mt-2 flex flex-col gap-1">
            {results.map((r) => (
              <li key={`${r.providerId}/${r.externalId}`} className="text-xs">
                <span
                  className={
                    r.status === 'failed'
                      ? 'text-red-700 dark:text-red-400'
                      : 'text-green-700 dark:text-green-400'
                  }
                >
                  {r.status === 'created' ? '新建' : r.status === 'completed' ? '补全' : '失败'}
                </span>
                <span className="ml-2 text-gray-700 dark:text-gray-300">{r.externalId}</span>
                {r.status === 'failed' && r.error ? (
                  <span className="ml-2 text-gray-500 dark:text-gray-400">{r.error}</span>
                ) : null}
              </li>
            ))}
          </ul>
        </div>
      ) : null}
    </section>
  );
}
