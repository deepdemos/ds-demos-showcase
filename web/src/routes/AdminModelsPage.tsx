import { useCallback, useState } from 'react';
import { useSearchParams } from 'react-router';

import { EmptyState } from '@/components/ui';
import type { ModelCatalogItem } from '@/api';
import { ModelBatch } from '@/features/admin/model-batch';
import { ModelDiff, type SelectedPayload } from '@/features/admin/model-diff';
import { ModelSearch, refOf } from '@/features/admin/model-search';
import { ModelSyncBar } from '@/features/admin/model-sync-bar';
import { useAuth } from '@/features/auth/AuthContext';

/**
 * 管理端「模型导入」。
 *
 * # 一页里做完四件事
 *
 * 同步（镜像有多旧 / 立刻拉一次）→ 搜索（8931 条里找一条）→ 差异（哪些字段该被动）
 * → 批量落库（逐条报结果）。四块缺一块，这条路就走不完：只给同步是运维视角，
 * 只给导入是开发者视角。
 *
 * # 为什么状态放在 URL 里
 *
 * `?q=` / `?offset=` / `?ref=provider/externalId` 都在 URL：刷新能复原、
 * 链接能发给别人复现、「返回」不会把搜索词弄丢。这也是作品库那页的口径。
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

export function AdminModelsPage() {
  const { me, isLoading } = useAuth();
  const [params, setParams] = useSearchParams();
  const [queue, setQueue] = useState<SelectedPayload[]>([]);
  // 选中的条目由 URL 决定，不用本地 state：刷新与分享链接都要能复原。
  const [picked, setPicked] = useState<ModelCatalogItem | null>(null);

  const q = params.get('q') ?? '';
  const includeDeprecated = params.get('deprecated') === '1';
  const offset = Number(params.get('offset') ?? '0') || 0;
  const refParam = params.get('ref');
  const ref = parseRef(refParam);

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
    (next: { q: string; includeDeprecated: boolean }) => {
      // 换搜索词必须回第一页：停在第 3 页会直接空白。
      patch({
        q: next.q === '' ? null : next.q,
        deprecated: next.includeDeprecated ? '1' : null,
        offset: null,
      });
    },
    [patch],
  );

  const onSelect = useCallback(
    (item: ModelCatalogItem) => {
      // 存一份列表行：它带来 canonicalId 与展示名，省一次回查。
      setPicked(item);
      patch({ ref: refOf(item) });
    },
    [patch],
  );

  if (isLoading) return null;

  // only 用来做「入口可见性」，不是权限判定——真正的判定在服务端。
  if (me === null) {
    return (
      <div className="mx-auto max-w-2xl px-4 py-10">
        <EmptyState title="请先登录" description="模型导入是管理端功能，需要先登录。" />
      </div>
    );
  }

  if (me.role !== 'admin') {
    return (
      <div className="mx-auto max-w-2xl px-4 py-10">
        <EmptyState
          title="没有导入权限"
          description="导入会写共享的模型目录，只有持有 model.import 权限的管理员可以操作。"
        />
      </div>
    );
  }

  return (
    <div className="mx-auto flex max-w-7xl flex-col gap-4 px-4 py-6">
      <header>
        <h1 className="text-xl font-semibold text-gray-900 dark:text-gray-100">模型导入</h1>
        <p className="mt-1 text-sm text-gray-500 dark:text-gray-400">
          从上游目录（models.dev）挑条目，逐字段确认后落库。写的是共享目录——一条改动会影响
          所有引用它的版本。
        </p>
      </header>

      <ModelSyncBar />

      <div className="grid gap-4 lg:grid-cols-2">
        <ModelSearch
          q={q}
          includeDeprecated={includeDeprecated}
          offset={offset}
          selectedRef={refParam}
          onSearch={onSearch}
          onOffset={(next) => patch({ offset: next === 0 ? null : String(next) })}
          onSelect={onSelect}
        />
        <ModelDiff
          target={ref}
          item={
            picked && ref && refOf(picked) === `${ref.providerId}/${ref.externalId}` ? picked : null
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
    </div>
  );
}
