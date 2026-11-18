import { useState } from 'react';
import { Link } from 'react-router';
import { Button, EmptyState, Select } from '@/components/ui';
import {
  clearHistory,
  readHistory,
  readLimit,
  removeEntry,
  writeLimit,
  type HistoryEntry,
} from '@/features/history/local-history';

/**
 * 浏览历史（#426 的第一半）。
 *
 * 数据全部来自本机 localStorage：未登录也能看（HIST-001），
 * 因此这条路由不挂登录守卫，也不请求任何接口。
 *
 * 为什么条目带标题/封面快照：见 local-history.ts 的注释——历史页没有批量端点，
 * 而作品被删之后回源只会 404，整页会因此变空。
 */

const LIMIT_OPTIONS = [
  { value: '20', label: '20 条' },
  { value: '50', label: '50 条' },
  { value: '100', label: '100 条' },
  { value: '200', label: '200 条' },
  { value: '500', label: '500 条' },
];

function formatViewedAt(iso: string): string {
  const at = new Date(iso);
  return Number.isNaN(at.getTime()) ? '时间未知' : at.toLocaleString('zh-CN', { hour12: false });
}

export function HistoryPage() {
  // 同步读 localStorage：首帧就有数据，不会先闪一下空态。
  const [entries, setEntries] = useState<HistoryEntry[]>(() => readHistory());
  const [limit, setLimit] = useState<number>(() => readLimit());

  const onLimitChange = (value: string) => {
    writeLimit(Number(value));
    // 下调上限时 writeLimit 已经裁剪过，这里按实际结果刷新。
    setLimit(readLimit());
    setEntries(readHistory());
  };

  const onRemove = (slug: string) => {
    removeEntry(slug);
    setEntries(readHistory());
  };

  const onClear = () => {
    clearHistory();
    setEntries([]);
  };

  return (
    <section className="mx-auto w-full max-w-3xl px-4 py-8">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold text-gray-900 dark:text-gray-100">浏览历史</h1>
          <p className="mt-1 text-sm text-gray-500 dark:text-gray-400">
            只保存在这个浏览器，未登录也能看；同一作品只留较近的一次。
          </p>
        </div>
        <div className="flex items-center gap-2 text-sm">
          <label htmlFor="history-limit" className="text-gray-600 dark:text-gray-300">
            本机上限
          </label>
          <Select
            id="history-limit"
            ariaLabel="本机历史上限"
            value={String(limit)}
            options={LIMIT_OPTIONS}
            onChange={onLimitChange}
          />
          <Button type="button" onClick={onClear} disabled={entries.length === 0}>
            清空
          </Button>
        </div>
      </div>

      <p className="mt-3 text-xs text-gray-500 dark:text-gray-400">
        共 {entries.length} 条（本机）
      </p>

      {entries.length === 0 ? (
        <div className="mt-4">
          <EmptyState
            title="还没有浏览记录"
            description="看过作品之后，这里会按时间倒序列出最近浏览过的作品。"
            action={
              <Link className="text-sm underline" to="/demos">
                去作品库看看
              </Link>
            }
          />
        </div>
      ) : (
        <ul className="mt-4 list-none space-y-3 p-0">
          {entries.map((entry) => (
            <li
              key={entry.slug}
              className="flex items-center gap-3 rounded-lg border border-gray-200 p-3 dark:border-gray-800"
            >
              {entry.coverUrl ? (
                <img
                  src={entry.coverUrl}
                  alt=""
                  className="h-16 w-24 flex-none rounded object-cover"
                  loading="lazy"
                />
              ) : (
                <div
                  aria-hidden="true"
                  className="h-16 w-24 flex-none rounded bg-gray-100 dark:bg-gray-800"
                />
              )}
              <div className="min-w-0 flex-1">
                <Link
                  to={'/demos/' + entry.slug}
                  className="block truncate text-base font-medium text-gray-900 hover:underline dark:text-gray-100"
                >
                  {entry.title || entry.slug}
                </Link>
                <p className="mt-1 text-xs text-gray-500 dark:text-gray-400">
                  本机 · {formatViewedAt(entry.viewedAt)}
                </p>
              </div>
              <Button
                type="button"
                aria-label={'删除 ' + (entry.title || entry.slug)}
                onClick={() => onRemove(entry.slug)}
              >
                删除
              </Button>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
