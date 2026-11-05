import { useCallback, useEffect, useState } from 'react';
import { Button, ErrorState, Skeleton } from '@/components/ui';
import {
  listDemoRevisions,
  restoreDemoRevision,
  type DemoRevision,
  type Error as ApiError,
} from '@/api';

const SOURCE_LABEL: Record<string, string> = {
  upload: '上传',
  url_import: '链接导入',
  rollback: '回滚',
};

type Loaded =
  { kind: 'ok'; items: DemoRevision[] } | { kind: 'missing' } | { kind: 'error'; message: string };

async function load(slug: string): Promise<Loaded> {
  const response = await listDemoRevisions(slug);
  if (response.status === 200) return { kind: 'ok', items: response.data.items };
  if (response.status === 404) return { kind: 'missing' };
  const body = response.data as ApiError;
  return { kind: 'error', message: body.message ?? `读取版本失败：HTTP ${response.status}` };
}

/**
 * 版本历史。
 *
 * # 为什么回滚按钮不在「当前版本」那一行
 *
 * 回滚到当前版本没有意义，服务端会直接 400。前端在这里也把它藏起来，
 * 是为了别让作者点出一个必然失败的按钮——但**判断依据仍然是服务端返回的 isCurrent**，
 * 不是「第一行」这种位置推断：版本顺序与「是不是当前版本」是两件事，
 * 回滚之后当前版本会在第一行，而作者改稿改到一半时它们可能不重合。
 *
 * # 为什么回滚完要重新加载
 *
 * 回滚产生的是**新版本**，列表会多一行，而且原「当前版本」标记会挪位置。
 * 就地改本地状态容易和下一次刷新后的结果不一致，整页重载最省事也最诚实。
 */
export function RevisionHistory({ slug }: { slug: string }) {
  const [state, setState] = useState<Loaded | null>(null);
  const [busyRevNo, setBusyRevNo] = useState<number | null>(null);
  const [error, setError] = useState<string | null>(null);

  const fetchList = useCallback(async () => {
    setState(null);
    setState(await load(slug));
  }, [slug]);

  useEffect(() => {
    void fetchList();
  }, [fetchList]);

  const onRestore = async (revNo: number) => {
    setBusyRevNo(revNo);
    setError(null);
    try {
      const response = await restoreDemoRevision(slug, revNo);
      if (response.status === 200) {
        window.location.reload();
        return;
      }
      const body = response.data as ApiError;
      setError(body.message ?? `回滚失败：HTTP ${response.status}`);
    } catch {
      setError('回滚失败，请检查网络后重试');
    } finally {
      setBusyRevNo(null);
    }
  };

  if (!state) {
    return (
      <section className="mt-10">
        <h2 className="text-lg font-medium">版本历史</h2>
        <div className="mt-3">
          <Skeleton lines={3} lineHeight="h-6" />
        </div>
      </section>
    );
  }

  if (state.kind === 'error') {
    return (
      <section className="mt-10">
        <h2 className="text-lg font-medium">版本历史</h2>
        <div className="mt-3">
          <ErrorState
            title="读取版本失败"
            description={state.message}
            onRetry={() => void fetchList()}
          />
        </div>
      </section>
    );
  }

  if (state.kind === 'missing') {
    return (
      <section className="mt-10">
        <h2 className="text-lg font-medium">版本历史</h2>
        <p className="mt-2 text-sm text-gray-600 dark:text-gray-300">读不到这件作品的版本。</p>
      </section>
    );
  }

  return (
    <section className="mt-10">
      <h2 className="text-lg font-medium">版本历史</h2>
      <p className="mt-1 text-sm text-gray-500 dark:text-gray-400">
        回滚不是撤销：它会追加一个新版本，同样要过复审才会对外生效。
      </p>

      {error && (
        <p className="mt-2 text-sm text-red-600 dark:text-red-400" role="alert">
          {error}
        </p>
      )}

      <ul className="mt-3 flex list-none flex-col gap-2 p-0">
        {state.items.map((rev) => (
          <li
            key={rev.revNo}
            className="flex flex-wrap items-center gap-2 rounded-lg border border-gray-200 px-3 py-2 text-sm dark:border-gray-800"
          >
            <span className="font-medium">第 {rev.revNo} 版</span>
            {rev.isCurrent && (
              <span className="rounded bg-blue-100 px-1.5 py-0.5 text-xs text-blue-700 dark:bg-blue-950 dark:text-blue-300">
                当前版本
              </span>
            )}
            {rev.isPublished && (
              <span className="rounded bg-green-100 px-1.5 py-0.5 text-xs text-green-700 dark:bg-green-950 dark:text-green-300">
                对外版本
              </span>
            )}
            {rev.deleted && (
              <span className="rounded bg-gray-100 px-1.5 py-0.5 text-xs text-gray-600 dark:bg-gray-800 dark:text-gray-300">
                已删除
              </span>
            )}
            {rev.erased && (
              <span className="rounded bg-gray-100 px-1.5 py-0.5 text-xs text-gray-600 dark:bg-gray-800 dark:text-gray-300">
                已擦除
              </span>
            )}
            <span className="text-gray-600 dark:text-gray-300">{rev.title || '（无标题）'}</span>
            <span className="text-xs text-gray-500 dark:text-gray-400">
              {SOURCE_LABEL[rev.source] ?? rev.source} ·{' '}
              {new Date(rev.createdAt).toLocaleString('zh-CN')}
            </span>
            {!rev.isCurrent && !rev.erased && (
              <span className="ml-auto">
                <Button
                  type="button"
                  disabled={busyRevNo !== null}
                  onClick={() => void onRestore(rev.revNo)}
                >
                  {busyRevNo === rev.revNo ? '回滚中…' : '回滚到这一版'}
                </Button>
              </span>
            )}
          </li>
        ))}
      </ul>
    </section>
  );
}
