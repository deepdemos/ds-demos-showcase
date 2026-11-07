import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';

import { Button, ErrorState, Skeleton } from '@/components/ui';
import { getModelSyncStatus, syncModelCatalog, type ModelSyncStatus } from '@/api';

/**
 * 顶部同步条：镜像有多旧 + 立即同步。
 *
 * # 为什么读状态是单独一个端点
 *
 * 触发同步的 `POST` 只回答「这一次的结果」。页面刷新之后就没得看了，而运维真正
 * 关心的是「镜像现在有多旧、上次为什么失败」——那是 `model_source_sync` 的记账，
 * 必须能单独读（`GET /admin/models/sync`）。
 *
 * # 为什么 `status = never` 不是错误
 *
 * 「从没同步过」是新库的初始状态，不是故障。用一条红色错误来表达它，会让人
 * 以为服务坏了，而不是去点右边那个按钮。
 */

const STATUS_TEXT: Record<string, { label: string; className: string }> = {
  ok: { label: '已同步', className: 'text-green-700 dark:text-green-400' },
  stale: { label: '需要同步', className: 'text-amber-700 dark:text-amber-400' },
  failed: { label: '上次同步失败', className: 'text-red-700 dark:text-red-400' },
  never: { label: '从未同步过', className: 'text-gray-600 dark:text-gray-400' },
};

function when(value: string | null | undefined): string {
  if (!value) return '—';
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) return '—';
  return d.toLocaleString('zh-CN', { hour12: false });
}

/** 把状态行压成一句人话。空值一律显示「—」，不要留下 `null` 三个字母。 */
export function syncSummary(status: ModelSyncStatus): string {
  const parts = [`${status.items.toLocaleString('zh-CN')} 条`];
  parts.push(`上次成功 ${when(status.lastSuccessAt)}`);
  if (status.lastAttemptAt && status.lastAttemptAt !== status.lastSuccessAt) {
    parts.push(`上次尝试 ${when(status.lastAttemptAt)}`);
  }
  return parts.join(' · ');
}

export function ModelSyncBar() {
  const queryClient = useQueryClient();
  const [notice, setNotice] = useState<string | undefined>();

  const status = useQuery({
    queryKey: ['admin', 'models', 'sync-status'],
    queryFn: async () => {
      const res = await getModelSyncStatus();
      // orval 生成的函数对非 2xx **不 throw**，只把 status 带回来。不自己判，
      // `isError` 永远是 false，403 / 503 就变成了「加载中」。
      if (res.status !== 200) throw new Error(`同步状态读取失败（HTTP ${res.status}）`);
      return res.data;
    },
  });

  const sync = useMutation({
    mutationFn: async () => {
      const res = await syncModelCatalog();
      if (res.status === 200) return res.data;
      if (res.status === 409) throw new Error('另一个同步正在进行，稍等一会儿再试');
      if (res.status === 502) throw new Error('上游不可达，本地镜像保持原样');
      throw new Error(`同步失败（HTTP ${res.status}）`);
    },
    onSuccess: (data) => {
      setNotice(
        data.skipped
          ? `上游内容没有变化，镜像保持 ${data.items.toLocaleString('zh-CN')} 条`
          : `已同步 ${data.items.toLocaleString('zh-CN')} 条`,
      );
      void queryClient.invalidateQueries({ queryKey: ['admin', 'models'] });
    },
    onError: (err: Error) => setNotice(err.message),
  });

  const shape = status.data ? STATUS_TEXT[status.data.status] : undefined;

  return (
    <section className="rounded-xl border border-gray-200 p-4 dark:border-gray-800">
      <div className="flex flex-wrap items-center gap-x-4 gap-y-2">
        <h2 className="text-sm font-medium text-gray-900 dark:text-gray-100">上游目录镜像</h2>
        {status.isPending ? <Skeleton lines={1} lineHeight="h-4" /> : null}
        {status.data && shape ? (
          <>
            <span className={`text-sm font-medium ${shape.className}`}>{shape.label}</span>
            <span className="text-xs text-gray-600 dark:text-gray-400">
              {syncSummary(status.data)}
            </span>
          </>
        ) : null}
        <span className="ml-auto" />
        <Button onClick={() => sync.mutate()} disabled={sync.isPending}>
          {sync.isPending ? '同步中…' : '立即同步'}
        </Button>
      </div>
      {status.isError ? (
        <ErrorState
          title="同步状态读取失败"
          description={status.error instanceof Error ? status.error.message : undefined}
          onRetry={() => void status.refetch()}
          retrying={status.isFetching}
        />
      ) : null}
      {status.data?.status === 'failed' && status.data.error ? (
        <p role="alert" className="mt-2 text-xs text-red-700 dark:text-red-400">
          上次失败原因：{status.data.error}
        </p>
      ) : null}
      {notice ? (
        <p role="status" className="mt-2 text-xs text-gray-700 dark:text-gray-300">
          {notice}
        </p>
      ) : null}
    </section>
  );
}
