import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useMemo, useState } from 'react';
import { Link } from 'react-router';
import { Button, Checkbox, EmptyState, Pagination, Select, Skeleton } from '@/components/ui';
import { deleteMyHistory, getMyHistory, updateMyHistorySettings } from '@/api';
import { useAuth } from '@/features/auth/AuthContext';
import {
  clearHistory,
  readHistory,
  readLimit,
  removeEntry,
  writeLimit,
  type HistoryEntry,
} from '@/features/history/local-history';

/**
 * 浏览历史（#426，两个来源合并）。
 *
 * # 两份来源
 *
 * - 本机：localStorage 里的快照，未登录也有（HIST-001）。
 * - 账号：GET /me/history，只有登录且开关打开时才有；关闭只停新增（HIST-008）。
 *
 * 合并规则：按 slug 去重、取**较近的一次**（HIST-004/005），每行标出来源
 * （HIST-002）。两边都有时，时间近的那条决定标题与时刻，标题/封面为空时用另一边的快照补。
 *
 * # 为什么一次取完账号历史
 *
 * 账号上限默认 200，最多两页（pageSize=100）。合并展示要先排序再分页，只拿第一页
 * 会让「最近浏览」被切在半页上；所以这里拉完（最多 10 页防御）再在客户端分页。
 */

const PAGE_SIZE = 20;

const LIMIT_OPTIONS = [
  { value: '20', label: '20 条' },
  { value: '50', label: '50 条' },
  { value: '100', label: '100 条' },
  { value: '200', label: '200 条' },
  { value: '500', label: '500 条' },
];

type Source = 'local' | 'account';

interface MergedEntry {
  slug: string;
  title: string | null;
  coverUrl: string | null;
  viewedAt: string;
  source: Source;
  /** 本机是否也有这条（决定能不能「删除本机记录」）。 */
  inLocal: boolean;
}

function formatViewedAt(iso: string): string {
  const at = new Date(iso);
  return Number.isNaN(at.getTime()) ? '时间未知' : at.toLocaleString('zh-CN', { hour12: false });
}

function localEntriesToMerged(entries: HistoryEntry[]): MergedEntry[] {
  return entries.map((entry) => ({
    slug: entry.slug,
    title: entry.title || null,
    coverUrl: entry.coverUrl,
    viewedAt: entry.viewedAt,
    source: 'local',
    inLocal: true,
  }));
}

function mergeSources(local: HistoryEntry[], account: MergedEntry[]): MergedEntry[] {
  const bySlug = new Map<string, MergedEntry>();
  for (const item of account) bySlug.set(item.slug, item);

  for (const entry of localEntriesToMerged(local)) {
    const current = bySlug.get(entry.slug);
    if (!current || Date.parse(entry.viewedAt) > Date.parse(current.viewedAt)) {
      bySlug.set(entry.slug, {
        ...entry,
        // 时间近的是本机，但标题/封面可能只有账号那边有（或反过来）。
        title: entry.title ?? current?.title ?? null,
        coverUrl: entry.coverUrl ?? current?.coverUrl ?? null,
      });
    } else {
      bySlug.set(entry.slug, {
        ...current,
        title: current.title ?? entry.title,
        coverUrl: current.coverUrl ?? entry.coverUrl,
        inLocal: true,
      });
    }
  }

  return [...bySlug.values()].sort((a, b) => Date.parse(b.viewedAt) - Date.parse(a.viewedAt));
}

function accountEntries(
  items: { slug: string; title: string | null; coverUrl: string | null; viewedAt: string }[],
): MergedEntry[] {
  return items.map((item) => ({
    slug: item.slug,
    title: item.title,
    coverUrl: item.coverUrl,
    viewedAt: item.viewedAt,
    source: 'account',
    inLocal: false,
  }));
}

/**
 * 取完账号历史（最多 10 页，防御管理员把上限调到很大）。
 * 契约里 pageSize 最大 100；每次失败都抛，页面统一落错误态。
 */
async function fetchAccountHistory() {
  const first = await getMyHistory({ page: 1, pageSize: 100 });
  if (first.status === 401) {
    throw new Error('登录已过期，请重新登录');
  }
  if (first.status !== 200) {
    throw new Error('账号历史加载失败（' + first.status + '）');
  }

  const items = [...first.data.items];
  let page = 1;
  while (items.length < first.data.total && page < 10) {
    page += 1;
    const next = await getMyHistory({ page, pageSize: 100 });
    if (next.status !== 200 || next.data.items.length === 0) break;
    items.push(...next.data.items);
  }
  return { ...first.data, items };
}

export function HistoryPage() {
  const { me, isLoading: authLoading } = useAuth();
  const queryClient = useQueryClient();

  const [localEntries, setLocalEntries] = useState<HistoryEntry[]>(() => readHistory());
  const [limit, setLimit] = useState<number>(() => readLimit());
  const [confirmingClear, setConfirmingClear] = useState(false);
  const [page, setPage] = useState(1);
  const [actionError, setActionError] = useState<string | null>(null);

  const account = useQuery({
    queryKey: ['me', 'history'],
    queryFn: fetchAccountHistory,
    enabled: me !== null,
  });

  const toggle = useMutation({
    mutationFn: async (enabled: boolean) => {
      const response = await updateMyHistorySettings({ enabled });
      if (response.status !== 200) throw new Error('开关写入失败（' + response.status + '）');
      return response.data;
    },
    onSuccess: () => {
      setActionError(null);
      void queryClient.invalidateQueries({ queryKey: ['me', 'history'] });
    },
    onError: (error) => setActionError(error instanceof Error ? error.message : '开关写入失败'),
  });

  const clearAccount = useMutation({
    mutationFn: async () => {
      const response = await deleteMyHistory();
      if (response.status !== 204) throw new Error('清空账号历史失败（' + response.status + '）');
    },
    onSuccess: () => {
      setConfirmingClear(false);
      setActionError(null);
      void queryClient.invalidateQueries({ queryKey: ['me', 'history'] });
    },
    onError: (error) => setActionError(error instanceof Error ? error.message : '清空账号历史失败'),
  });

  // 未登录时不展示缓存里的账号条目：退出登录之后那些数据不该继续出现。
  const accountItems = useMemo(
    () => (me !== null && account.data ? accountEntries(account.data.items) : []),
    [me, account.data],
  );
  const merged = useMemo(
    () => mergeSources(localEntries, accountItems),
    [localEntries, accountItems],
  );

  const pageCount = Math.max(1, Math.ceil(merged.length / PAGE_SIZE));
  const currentPage = Math.min(page, pageCount);
  const visible = merged.slice((currentPage - 1) * PAGE_SIZE, currentPage * PAGE_SIZE);

  // 读失败时不能用 true 冒充服务端设置：勾选被改会把真实的 false 写回 true。
  const accountEnabled = account.data?.enabled ?? false;

  const onLimitChange = (value: string) => {
    writeLimit(Number(value));
    setLimit(readLimit());
    setLocalEntries(readHistory());
  };

  const onRemoveLocal = (slug: string) => {
    removeEntry(slug);
    setLocalEntries(readHistory());
  };

  const onClearLocal = () => {
    clearHistory();
    setLocalEntries([]);
  };

  return (
    <section className="mx-auto w-full max-w-3xl px-4 py-8">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold text-gray-900 dark:text-gray-100">浏览历史</h1>
          <p className="mt-1 text-sm text-gray-500 dark:text-gray-400">
            本机记录只保存在这个浏览器；登录后还会合并账号记录。同一作品只留较近的一次。
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
          <Button type="button" onClick={onClearLocal} disabled={localEntries.length === 0}>
            清空本机
          </Button>
        </div>
      </div>

      {!authLoading && me === null ? (
        <p className="mt-3 text-sm text-gray-500 dark:text-gray-400">
          登录后可以同步账号历史；本机记录不受影响。
          <Link className="ml-2 underline" to="/login?return_to=/me/history">
            去登录
          </Link>
        </p>
      ) : null}

      {me !== null ? (
        <section className="mt-4 space-y-2 rounded-lg border border-gray-200 p-4 dark:border-gray-800">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <Checkbox
              label="记录账号历史"
              checked={accountEnabled}
              indeterminate={false}
              disabled={!account.data || account.isPending || toggle.isPending}
              onChange={(event) => toggle.mutate(event.target.checked)}
            />
            <Button
              type="button"
              onClick={() => setConfirmingClear(true)}
              disabled={accountItems.length === 0 || clearAccount.isPending}
            >
              清空账号历史
            </Button>
          </div>
          <p className="text-xs text-gray-500 dark:text-gray-400">
            {account.isError
              ? '账号历史设置读取失败，先不要修改；重试后再看。'
              : accountEnabled
                ? '关闭只停止新增，已有记录会保留；清空需要单独确认。'
                : '账号历史已关闭，不再新增记录；已有记录仍在，可以清空。'}
          </p>

          {account.isError ? (
            <p role="alert" className="text-sm text-red-600 dark:text-red-400">
              {account.error instanceof Error ? account.error.message : '账号历史加载失败'}
            </p>
          ) : null}

          {actionError ? (
            <p role="alert" className="text-sm text-red-600 dark:text-red-400">
              {actionError}
            </p>
          ) : null}

          {confirmingClear ? (
            <div role="alert" className="space-y-2 rounded border border-amber-300 p-3 text-sm">
              <p>
                会删除全部账号浏览记录（{accountItems.length} 条），不可恢复；本机记录不受影响。
              </p>
              <div className="flex gap-2">
                <Button
                  danger
                  type="button"
                  onClick={() => clearAccount.mutate()}
                  disabled={clearAccount.isPending}
                >
                  确认清空
                </Button>
                <Button
                  type="button"
                  onClick={() => setConfirmingClear(false)}
                  disabled={clearAccount.isPending}
                >
                  取消
                </Button>
              </div>
            </div>
          ) : null}
        </section>
      ) : null}

      <p className="mt-3 text-xs text-gray-500 dark:text-gray-400">
        共 {merged.length} 条（去重后 · 本机 {localEntries.length} · 账号 {accountItems.length}）
      </p>

      {me !== null && account.isPending ? <Skeleton lines={1} /> : null}

      {merged.length === 0 ? (
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
        <>
          <ul className="mt-4 list-none space-y-3 p-0">
            {visible.map((entry) => (
              <li
                key={entry.slug}
                data-slug={entry.slug}
                data-source={entry.source}
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
                    {entry.source === 'account' ? '账号' : '本机'} ·{' '}
                    {formatViewedAt(entry.viewedAt)}
                  </p>
                </div>
                {entry.inLocal ? (
                  <Button
                    type="button"
                    aria-label={'删除本机记录 ' + (entry.title || entry.slug)}
                    onClick={() => onRemoveLocal(entry.slug)}
                  >
                    删除本机
                  </Button>
                ) : null}
              </li>
            ))}
          </ul>

          <Pagination page={currentPage} pageCount={pageCount} onPageChange={setPage} />
        </>
      )}
    </section>
  );
}
