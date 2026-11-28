import { useCallback, useState } from 'react';
import { useSearchParams } from 'react-router';

import type { UpstreamModel } from '@/api';
import { EmptyState } from '@/components/ui';
import { Page } from '@/components/layout/page';
import { UpstreamModelSelector, upstreamRef } from '@/components/upstream-model-selector';
import { ModelBatch } from '@/features/admin/model-batch';
import { ModelCreate } from '@/features/admin/model-create';
import { ModelDiff, type SelectedPayload } from '@/features/admin/model-diff';
import { ModelList, type AdminModelStatusFilter } from '@/features/admin/model-list';
import { ModelSyncBar } from '@/features/admin/model-sync-bar';
import { useAuth } from '@/features/auth/AuthContext';

/**
 * 管理端「模型导入」。
 *
 * # 一页里做完四件事
 *
 * 同步（镜像有多旧 / 立刻拉一次）→ 选择器（8931 条里找一条，标出「已收录 → 站内 slug」）
 * → 差异（哪些字段该被动）→ 批量落库（逐条报结果）。四块缺一块，这条路就走不完：
 * 只给同步是运维视角，只给导入是开发者视角。
 *
 * # 为什么状态放在 URL 里
 *
 * `?q=` / `?ref=provider/externalId` 都在 URL：刷新能复原、链接能发给别人复现、
 * 「返回」不会把搜索词弄丢。这也是作品库那页的口径。选择器自己只保管「正在输入的那串」。
 *
 * # 为什么权限不在这里判
 *
 * 谁能导入由服务端说了算（`model.import` 权限）。前端**不**读 `me.role` 去猜——
 * 一猜就会和权限表脱钩：以后把权限给一个新角色，前端还得跟着改。这里只做一件事：
 * 收到 403 就展示「没有导入权限」，其余交给后端。
 *
 * # 为什么非 admin 也进得来
 *
 * 这个路由不拦人，只是正文对没权限的人显示一句说明。重定向到首页会让人以为
 * 链接坏了——尤其是从别处复制过来的链接。
 */
/**
 * 把 `?ref=provider/externalId` 折成两段。
 *
 * 用 `/` 分隔是因为 `externalId` 里可能有 `:`（`:free` 这类渠道标记），
 * 而 provider 标识里不会有 `/`。格式不对就当没选——不要抛错，一条脏链接
 * 不该让整页白屏。
 */
export function parseRef(value: string | null): { providerId: string; externalId: string } | null {
  if (!value) return null;
  const at = value.indexOf('/');
  if (at <= 0 || at === value.length - 1) return null;
  return { providerId: value.slice(0, at), externalId: value.slice(at + 1) };
}

const VALID_STATUS = new Set<string>(['candidate', 'active', 'unverified', 'deprecated']);

function parseStatus(value: string | null): AdminModelStatusFilter {
  if (value && VALID_STATUS.has(value)) return value as AdminModelStatusFilter;
  return '';
}

export function AdminModelsPage() {
  const { me, isLoading } = useAuth();
  const [params, setParams] = useSearchParams();
  const [queue, setQueue] = useState<SelectedPayload[]>([]);
  // 选中的条目由 URL 决定，不用本地 state：刷新与分享链接都要能复原。
  const [picked, setPicked] = useState<UpstreamModel | null>(null);

  const q = params.get('q') ?? '';
  const refParam = params.get('ref');
  const ref = parseRef(refParam);
  const status = parseStatus(params.get('status'));
  const page = Math.max(1, Math.floor(Number(params.get('page')) || 1));

  const patch = useCallback(
    (next: Record<string, string | null>) => {
      const merged = new URLSearchParams(params);
      for (const [key, value] of Object.entries(next)) {
        if (value === null || value === '') merged.delete(key);
        else merged.set(key, value);
      }
      setParams(merged, { replace: true });
    },
    [params, setParams],
  );

  const onSearch = useCallback(
    (next: { q: string }) => {
      patch({ q: next.q === '' ? null : next.q });
    },
    [patch],
  );

  const onSelect = useCallback(
    (item: UpstreamModel) => {
      // 存一份行：它带来 canonicalId、source 与展示名，省一次回查。
      setPicked(item);
      patch({ ref: upstreamRef(item) });
    },
    [patch],
  );

  const onStatusChange = useCallback(
    (next: AdminModelStatusFilter) => {
      patch({ status: next === '' ? null : next, page: null });
    },
    [patch],
  );

  const onPageChange = useCallback(
    (next: number) => {
      patch({ page: next <= 1 ? null : String(next) });
    },
    [patch],
  );

  if (isLoading) return null;

  // only 用来做「入口可见性」，不是权限判定——真正的判定在服务端。
  if (me === null) {
    return (
      <Page title="模型导入">
        <EmptyState title="请先登录" description="模型导入是管理端功能，需要先登录。" />
      </Page>
    );
  }

  if (me.role !== 'admin') {
    return (
      <Page title="模型导入">
        <EmptyState
          title="没有导入权限"
          description="导入会写共享的模型目录，只有持有 model.import 权限的管理员可以操作。"
        />
      </Page>
    );
  }

  return (
    <Page
      title="模型导入"
      description="从上游目录（models.dev）挑条目，逐字段确认后落库。写的是共享目录——一条改动会影响所有引用它的版本。"
    >
      <ModelSyncBar />

      <div className="grid gap-4 lg:grid-cols-2">
        <UpstreamModelSelector
          q={q}
          selectedRef={refParam}
          onSearch={onSearch}
          onSelect={onSelect}
        />
        <ModelDiff
          target={ref}
          item={
            picked && ref && upstreamRef(picked) === `${ref.providerId}/${ref.externalId}`
              ? picked
              : null
          }
          enqueued={
            ref !== null &&
            queue.some(
              (q) => q.ref.providerId === ref.providerId && q.ref.externalId === ref.externalId,
            )
          }
          onEnqueue={(payload) => setQueue((prev) => [...prev, payload])}
        />
      </div>

      <ModelBatch
        queue={queue}
        onRemove={(index) => setQueue((prev) => prev.filter((_, i) => i !== index))}
        onClear={() => setQueue([])}
      />

      <ModelList
        status={status}
        page={page}
        onStatusChange={onStatusChange}
        onPageChange={onPageChange}
      />

      {/* 上游目录里没有的私有模型只能手建。放在这一页是因为它是管理端唯一的模型入口；
          这两块（导入 / 手动新建）在信息架构上是「模型维护」的两半，下一版会拆页。 */}
      <ModelCreate />
    </Page>
  );
}
