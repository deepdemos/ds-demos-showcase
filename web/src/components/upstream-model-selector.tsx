import { useQuery } from '@tanstack/react-query';
import { useEffect, useState } from 'react';

import { searchUpstreamModels, type UpstreamModel } from '@/api';
import { Button, EmptyState, ErrorState, Input, Skeleton } from '@/components/ui';

// 上游条目选择器：管理端导入页用它挑一条上游条目（#589），投稿页的三层选择器复用同一份
// 上游层（#590）。
//
// # 为什么不进控件库
//
// 它带着网络请求与业务指称（上游三元组、站内 slug），而控件库的契约是「无业务依赖、
// 能在 Dev Room 全量演示」。硬塞进去就得给控件库开一个「需要登录态与数据库」的特例，
// 而那条特例会一直留在门禁里。控件库照旧只放 Input / Button 这些无业务件。
//
// # 为什么搜的是公开端点
//
// `GET /api/v1/models/upstream`（#589）就是为选择器建的：不分页、最多 20 条，每条带
// `slug`——已收录时是站内模型 slug，未收录是 null。导入页要在这儿看到「这条导过了吗」，
// 投稿页要据此把条目提到第一层。判定在服务端只算一次（`model_offerings` 的认领行），
// 前端不重算，也不另写一份「按名字猜收录」。
//
// # 为什么关键词由调用方保管（props 的 q）
//
// URL 是筛选条件的唯一事实源（与作品库那页同口径）：刷新能复原、链接能发给别人复现。
// 组件自己再存一份，就会出现「地址栏说 deepseek、输入框说 qwen」的漂移。
// 组件内只有 draft（正在输入的那串），防抖后交回调用方。

/** 上游条目的稳定指称。用 `/` 分隔：provider 标识里不会有 `/`，externalId 可能有 `:`。 */
export function upstreamRef(item: Pick<UpstreamModel, 'providerId' | 'externalId'>): string {
  return `${item.providerId}/${item.externalId}`;
}

export interface UpstreamModelSelectorProps {
  /** 当前生效的搜索词（来自 URL）。 */
  q: string;
  /** 选中的条目指称，用于高亮；未选为 null。 */
  selectedRef: string | null;
  /** 防抖后的搜索词交回调用方（由它写 URL）。 */
  onSearch: (next: { q: string }) => void;
  onSelect: (item: UpstreamModel) => void;
  disabled?: boolean;
}

export function UpstreamModelSelector({
  q,
  selectedRef,
  onSearch,
  onSelect,
  disabled,
}: UpstreamModelSelectorProps) {
  const [draft, setDraft] = useState(q);

  // URL 变了输入框要跟着变（后退、分享链接、清空）。
  useEffect(() => setDraft(q), [q]);

  // 300ms 防抖：这个框的用法是「边打边看」，回车才搜会让每次修改都变成一次手动提交。
  useEffect(() => {
    if (draft === q) return;
    const timer = window.setTimeout(() => onSearch({ q: draft }), 300);
    return () => window.clearTimeout(timer);
  }, [draft, q, onSearch]);

  const upstream = useQuery({
    // 挂在 ['models'] 之下：导入成功之后 invalidateQueries(['models','upstream'])
    // 就能让这里的「未收录」翻成「已收录」，不必等 staleTime。
    queryKey: ['models', 'upstream', q],
    queryFn: async () => {
      const res = await searchUpstreamModels({ q: q === '' ? undefined : q });
      if (res.status !== 200) {
        throw new Error(`目录读取失败（HTTP ${res.status}）`);
      }
      return res.data.items;
    },
  });

  return (
    <section className="flex min-h-0 flex-col gap-3 rounded-xl border border-gray-200 p-4">
      <div className="flex flex-col gap-2">
        <Input
          aria-label="搜索上游条目"
          placeholder="按名字 / id / 厂商搜索"
          value={draft}
          maxLength={100}
          disabled={disabled}
          onChange={(e) => setDraft((e.target as HTMLInputElement).value)}
        />
        <p className="text-xs text-gray-500">
          不分页，一次最多 20 条；已收录的条目会标出站内 slug。
        </p>
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto">
        {upstream.isError ? (
          <ErrorState
            title="目录读取失败"
            description={upstream.error instanceof Error ? upstream.error.message : undefined}
            onRetry={() => void upstream.refetch()}
            retrying={upstream.isFetching}
          />
        ) : upstream.isPending ? (
          <Skeleton lines={6} lineHeight="h-10" />
        ) : upstream.data.length === 0 ? (
          <EmptyState
            title="没有匹配的条目"
            description="换个搜索词。如果上游目录还没同步过，先点上面的「立即同步」。"
          />
        ) : (
          <ul className="flex flex-col divide-y divide-gray-100">
            {upstream.data.map((item) => {
              const ref = upstreamRef(item);
              const active = ref === selectedRef;
              return (
                <li key={ref}>
                  {/* 用控件库的 Button 而不是原生 button：页面与页面级组件一律走控件库，
                      ui-contract 的门禁也按这条扫。 */}
                  <Button
                    onClick={() => onSelect(item)}
                    disabled={disabled}
                    aria-current={active ? 'true' : undefined}
                    className={[
                      'w-full !justify-start px-2 py-2 text-left font-normal',
                      active ? 'bg-blue-50' : 'hover:bg-gray-50',
                    ].join(' ')}
                  >
                    <span className="flex items-center gap-2">
                      <span className="truncate text-sm text-gray-900">{item.name}</span>
                      {item.slug ? (
                        <span className="shrink-0 rounded bg-green-50 px-1.5 py-0.5 text-[11px] text-green-700">
                          已收录 → {item.slug}
                        </span>
                      ) : (
                        <span className="shrink-0 rounded bg-gray-100 px-1.5 py-0.5 text-[11px] text-gray-600">
                          未收录
                        </span>
                      )}
                      {item.upstreamStatus === 'deprecated' ? (
                        <span className="shrink-0 rounded bg-amber-50 px-1.5 py-0.5 text-[11px] text-amber-700">
                          上游弃用
                        </span>
                      ) : null}
                    </span>
                    <span className="mt-0.5 block truncate text-xs text-gray-500">
                      {item.providerName} · {item.externalId}
                    </span>
                  </Button>
                </li>
              );
            })}
          </ul>
        )}
      </div>
    </section>
  );
}
