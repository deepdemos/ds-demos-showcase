import { useEffect, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Link, useSearchParams } from 'react-router';
import { Button, EmptyState, ErrorState, Input, Pagination, Skeleton } from '@/components/ui';
import { listDemos, type DemoList } from '@/api';

const PAGE_SIZE = 12;

/** 页码只认正整数，其余一律回第 1 页——URL 是用户能改的，不能假设它干净。 */
function readPage(raw: string | null): number {
  const n = Number(raw);
  return Number.isInteger(n) && n >= 1 ? n : 1;
}

async function fetchDemos(page: number, q: string): Promise<DemoList> {
  const response = await listDemos({
    page,
    pageSize: PAGE_SIZE,
    sort: 'latest',
    // 空串不传：URL 里带一个空 q 会让缓存键多分出一份，也让「清空搜索」看起来没生效。
    ...(q ? { q } : {}),
  });
  if (response.status === 200) return response.data;
  // 不把后端返回的 message 直接抛给界面：它可能带内部细节。
  throw new Error(`列表加载失败（HTTP ${response.status}）`);
}

export function DemosPage() {
  const [searchParams, setSearchParams] = useSearchParams();
  const page = readPage(searchParams.get('page'));
  // 搜索词放 URL：这样搜到的页面能分享、刷新不丢、浏览器后退能回到上一次搜索。
  const q = (searchParams.get('q') ?? '').trim();
  const [draft, setDraft] = useState(q);

  // URL 变了（后退、点链接）就把输入框同步过来；否则输入框会停在旧词上，
  // 看起来像「搜索没生效」。
  useEffect(() => {
    setDraft(q);
  }, [q]);

  const { data, isPending, isError, error, refetch, isFetching } = useQuery({
    queryKey: ['demos', page, q],
    queryFn: () => fetchDemos(page, q),
  });

  const goToPage = (next: number) => {
    const params = new URLSearchParams(searchParams);
    if (next <= 1) params.delete('page');
    else params.set('page', String(next));
    setSearchParams(params);
  };

  const submitSearch = (event: React.FormEvent) => {
    event.preventDefault();
    const params = new URLSearchParams(searchParams);
    const next = draft.trim();
    if (next) params.set('q', next);
    else params.delete('q');
    // 换搜索词必须回第 1 页：停在第 3 页会得到一片空白，看起来像搜不到。
    params.delete('page');
    setSearchParams(params);
  };

  return (
    <section className="mx-auto w-full max-w-6xl px-4 py-8">
      <header className="mb-6">
        <h1 className="text-2xl font-semibold">作品库</h1>
        <p className="mt-1 text-sm text-gray-500 dark:text-gray-400">
          {q ? `搜索「${q}」，按发布时间从新到旧。` : '按发布时间从新到旧。'}
        </p>

        <form className="mt-4 flex max-w-xl items-end gap-2" onSubmit={submitSearch} role="search">
          <label className="flex flex-1 flex-col gap-1 text-sm">
            关键词
            <Input
              type="search"
              value={draft}
              onChange={(e) => setDraft(e.target.value)}
              placeholder="标题或简介里的词"
              maxLength={100}
              aria-label="搜索作品"
            />
          </label>
          <Button type="submit" primary>
            搜索
          </Button>
          {q ? (
            <Button
              type="button"
              onClick={() => {
                setDraft('');
                const params = new URLSearchParams(searchParams);
                params.delete('q');
                params.delete('page');
                setSearchParams(params);
              }}
            >
              清除
            </Button>
          ) : null}
        </form>
      </header>

      {isPending && <Skeleton lines={6} lineHeight="h-24" />}

      {isError && (
        <ErrorState
          title="作品列表加载失败"
          description={error instanceof Error ? error.message : undefined}
          onRetry={() => void refetch()}
          retrying={isFetching}
        />
      )}

      {data &&
        data.items.length === 0 &&
        (q ? (
          <EmptyState
            title="没有匹配的作品"
            description={`「${q}」没有匹配到任何作品。换个词，或者清除搜索看全部。`}
          />
        ) : (
          <EmptyState
            title="还没有公开的作品"
            description="第一个作品发布并通过审核后，会出现在这里。"
          />
        ))}

      {data && data.items.length > 0 && (
        <>
          <ul className="m-0 grid list-none grid-cols-1 gap-4 p-0 sm:grid-cols-2 lg:grid-cols-3">
            {data.items.map((demo) => (
              <li key={demo.slug}>
                <Link
                  to={`/demos/${demo.slug}`}
                  className="flex h-full flex-col rounded-xl border border-gray-200 bg-white p-4 text-inherit no-underline hover:border-gray-300 dark:border-gray-800 dark:bg-gray-900 dark:hover:border-gray-700"
                >
                  {/* 封面区固定 16:9：有没有封面都占同样高度，否则网格会参差不齐 */}
                  <div className="mb-3 flex aspect-video items-center justify-center overflow-hidden rounded-lg bg-gray-100 text-xs text-gray-400 dark:bg-gray-800 dark:text-gray-500">
                    {demo.coverUrl ? (
                      <img src={demo.coverUrl} alt="" className="h-full w-full object-cover" />
                    ) : (
                      '无封面'
                    )}
                  </div>
                  <h2 className="line-clamp-2 min-h-12 text-base font-medium text-gray-900 dark:text-gray-100">
                    {demo.title ?? '（无标题）'}
                  </h2>
                  {demo.matchedIn === 'description' ? (
                    // 只有简介命中的时候，标题里根本看不到关键词——
                    // 不标出来，用户会以为「搜出来的东西不相关」。
                    <p className="mt-1 text-xs text-amber-700 dark:text-amber-500">简介命中</p>
                  ) : null}
                  <p className="mt-auto pt-3 text-xs text-gray-500 dark:text-gray-400">
                    {new Date(demo.createdAt).toLocaleDateString('zh-CN')}
                  </p>
                </Link>
              </li>
            ))}
          </ul>

          <div className="mt-4 flex items-center justify-between">
            <span className="text-sm text-gray-500 dark:text-gray-400">共 {data.total} 个作品</span>
            <Pagination
              page={data.page}
              pageCount={Math.max(1, Math.ceil(data.total / data.pageSize))}
              onPageChange={goToPage}
              disabled={isFetching}
            />
          </div>
        </>
      )}
    </section>
  );
}
