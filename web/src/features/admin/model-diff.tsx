import { useQuery } from '@tanstack/react-query';
import { useEffect, useState } from 'react';

import { Button, Checkbox, EmptyState, ErrorState, Input, Skeleton } from '@/components/ui';
import { previewModelImport, type ModelCatalogItem, type ModelImportField } from '@/api';

/**
 * 右侧：差异面板（preview → 勾字段 → 应用）。
 *
 * # 为什么必须先 preview 再 apply
 *
 * 上游数据不能直接进库：它会把 `deepseek-v4-flash:free` 这种渠道标记写在名字里、
 * 会丢字段、会把 description 写成营销话术。预览是把「上游说什么」和「本地现在是
 * 什么」摆在一起，让人决定哪些字段该被动。
 *
 * # 为什么「不可勾」的三种原因要分开写
 *
 * 值相同（no-op）、`source=manual`（人工写过，同步不覆盖）、`locked`（已锁定）
 * 是三件不同的事。合成一句「不可选」，管理员只能猜——而猜错的代价是覆盖掉
 * 别人手工维护的内容。
 *
 * # 为什么 `conflict` 不给「应用」按钮
 *
 * 该上游条目已经被另一个本地模型认领。再导一次要么覆盖那个模型的引用，
 * 要么静默失败。让它先归并（`entity.merge` 那条路），不要在这里给一个
 * 会覆盖别人认领关系的按钮。
 */

/** 不可勾选时给一句具体原因。顺序有意义：先解释「为什么灰」，再解释「谁写的」。 */
export function fieldReason(f: ModelImportField): string | null {
  if (f.locked) return '已锁定：人工锁住的字段不被同步覆盖';
  if (f.source === 'manual') return '人工写过：同步不覆盖人工内容';
  if (f.selectable) return null;
  return '与本地一致：没有可写的差异';
}

export interface SelectedPayload {
  ref: { providerId: string; externalId: string };
  fields: string[];
  label: string;
}

export interface ModelDiffProps {
  /**
   * 由 URL 的 `?ref=provider/externalId` 折出来；深链与刷新都靠它复原。
   *
   * **不能叫 `ref`**：React 把它当保留 prop，传给函数组件会被直接剥掉，
   * 组件里读到的永远是 undefined——而且不报错，表现为「点了没反应」。
   */
  target: { providerId: string; externalId: string } | null;
  /** 列表里对应的行（可能还没加载出来）。只用来补 canonicalId 与展示名。 */
  item: ModelCatalogItem | null;
  onEnqueue: (payload: SelectedPayload) => void;
  enqueued: boolean;
}

export function ModelDiff({ target, item, onEnqueue, enqueued }: ModelDiffProps) {
  const [checked, setChecked] = useState<Record<string, boolean>>({});
  const [name, setName] = useState('');

  const preview = useQuery({
    queryKey: [
      'admin',
      'models',
      'preview',
      target ? `${target.providerId}/${target.externalId}` : '',
    ],
    enabled: target !== null,
    queryFn: async () => {
      const res = await previewModelImport({
        providerId: target!.providerId,
        externalId: target!.externalId,
        // canonicalId 只有列表行带得出来；深链进来时没有它，服务端按
        // (source, provider_id, external_id) 一样能定位。
        ...(item?.canonicalId ? { canonicalId: item.canonicalId } : {}),
        source: 'models.dev',
      });
      if (res.status !== 200) {
        throw new Error(
          res.status === 404
            ? '目录里已经没有这条了，重新同步一次再看'
            : `差异读取失败（HTTP ${res.status}）`,
        );
      }
      return res.data;
    },
  });

  // 每次换条目重置勾选：默认勾上「可以勾的」，也就是服务端认为有真实差异的那些。
  useEffect(() => {
    if (!preview.data) return;
    const next: Record<string, boolean> = {};
    for (const f of preview.data.fields) next[f.field] = f.selectable;
    setChecked(next);
    const upstreamName = preview.data.fields.find((f) => f.field === 'name')?.upstream ?? '';
    setName(preview.data.target?.name ?? item?.name ?? upstreamName);
  }, [preview.data, item]);

  if (!target) {
    return (
      <section className="rounded-xl border border-dashed border-gray-200 p-4 dark:border-gray-800">
        <EmptyState
          title="选一条上游条目"
          description="左边点一条，这里会列出「上游有、本地没有」以及「两边不一样」的字段。"
        />
      </section>
    );
  }

  const mode = preview.data?.mode;
  const conflict = preview.data?.conflictWith;
  const checkedFields = Object.entries(checked)
    .filter(([, on]) => on)
    .map(([field]) => field);

  return (
    <section className="flex min-h-0 flex-col gap-3 rounded-xl border border-gray-200 p-4 dark:border-gray-800">
      <header className="flex flex-wrap items-center gap-2">
        <h2 className="text-sm font-medium text-gray-900 dark:text-gray-100">
          {item?.name ?? preview.data?.target?.name ?? target.externalId}
        </h2>
        {mode ? (
          <span
            className={[
              'rounded px-1.5 py-0.5 text-[11px]',
              mode === 'create'
                ? 'bg-blue-50 text-blue-700 dark:bg-blue-950 dark:text-blue-300'
                : mode === 'conflict'
                  ? 'bg-red-50 text-red-700 dark:bg-red-950 dark:text-red-300'
                  : 'bg-amber-50 text-amber-700 dark:bg-amber-950 dark:text-amber-300',
            ].join(' ')}
          >
            {mode === 'create' ? '新建' : mode === 'conflict' ? '已被认领' : '补全'}
          </span>
        ) : null}
        <span className="text-xs text-gray-500 dark:text-gray-400">
          {item?.providerName ?? target.providerId} · {target.externalId}
        </span>
      </header>

      {preview.isError ? (
        <ErrorState
          title="差异读取失败"
          description={preview.error instanceof Error ? preview.error.message : undefined}
          onRetry={() => void preview.refetch()}
          retrying={preview.isFetching}
        />
      ) : preview.isPending ? (
        <Skeleton lines={8} />
      ) : (
        <>
          {mode === 'complete' && preview.data?.target ? (
            <p className="text-xs text-gray-600 dark:text-gray-400">
              补全到已有模型 <code>{preview.data.target.slug}</code>
            </p>
          ) : null}
          {mode === 'conflict' ? (
            <p
              role="alert"
              className="rounded-lg border border-red-300 bg-red-50 px-3 py-2 text-xs text-red-800 dark:border-red-900 dark:bg-red-950 dark:text-red-200"
            >
              该上游条目已被 <code>{conflict?.slug}</code> 认领。先把两条模型归并，再从这里导入。
            </p>
          ) : null}
          {mode === 'create' ? (
            <div className="flex flex-col gap-2">
              <label className="text-xs text-gray-600 dark:text-gray-400">
                展示名（默认取上游名，去掉 `:free` 这类渠道标记）
                <Input
                  aria-label="展示名"
                  value={name}
                  maxLength={128}
                  onChange={(e) => setName((e.target as HTMLInputElement).value)}
                />
              </label>
            </div>
          ) : null}

          <div className="min-h-0 flex-1 overflow-y-auto">
            <table className="w-full border-collapse text-sm">
              <thead>
                <tr className="text-left text-xs text-gray-500 dark:text-gray-400">
                  <th className="w-8 py-1" />
                  <th className="py-1">字段</th>
                  <th className="py-1">本地</th>
                  <th className="py-1">上游</th>
                  <th className="py-1">来源</th>
                </tr>
              </thead>
              <tbody>
                {preview.data?.fields.map((f) => {
                  const reason = fieldReason(f);
                  return (
                    <tr key={f.field} className="border-t border-gray-100 dark:border-gray-800">
                      <td className="py-1 align-top">
                        <Checkbox
                          label={`选择 ${f.field}`}
                          aria-label={`选择 ${f.field}`}
                          checked={Boolean(checked[f.field]) && reason === null}
                          disabled={reason !== null}
                          indeterminate={false}
                          onChange={(e) =>
                            setChecked((prev) => ({ ...prev, [f.field]: e.target.checked }))
                          }
                        />
                      </td>
                      <td className="py-1 align-top text-gray-900 dark:text-gray-100">{f.field}</td>
                      <td className="max-w-48 truncate py-1 align-top text-gray-600 dark:text-gray-400">
                        {f.current || '—'}
                      </td>
                      <td className="max-w-48 truncate py-1 align-top text-gray-900 dark:text-gray-100">
                        {f.upstream || '—'}
                      </td>
                      <td className="py-1 align-top text-xs text-gray-500 dark:text-gray-400">
                        {reason ?? (f.source === 'none' ? '本地没有' : f.source)}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>

          <div className="flex items-center gap-3">
            <Button
              disabled={mode === 'conflict' || checkedFields.length === 0 || enqueued}
              onClick={() =>
                onEnqueue({
                  ref: { providerId: target.providerId, externalId: target.externalId },
                  fields: checkedFields,
                  label:
                    mode === 'create'
                      ? name || target.externalId
                      : (preview.data?.target?.name ?? target.externalId),
                })
              }
            >
              {enqueued ? '已在待导入清单' : '加入待导入'}
            </Button>
            <span className="text-xs text-gray-500 dark:text-gray-400">
              已选 {checkedFields.length} 个字段
            </span>
          </div>
        </>
      )}
    </section>
  );
}
