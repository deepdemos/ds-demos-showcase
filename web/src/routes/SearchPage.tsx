import { useEffect, useState, type FormEvent } from 'react';
import { Link, useSearchParams } from 'react-router';
import { useQuery } from '@tanstack/react-query';
import { Button, EmptyState, ErrorState, Input, Pagination, Skeleton } from '@/components/ui';
import { Page } from '@/components/layout/page';
import { listDemos, type DemoList } from '@/api';

const PAGE_SIZE = 20;

export function SearchPage() {
  const [params, setParams] = useSearchParams();
  const q = (params.get('q') ?? '').trim();
  const page = Math.max(1, Number(params.get('page')) || 1);
  const [draft, setDraft] = useState(q);

  useEffect(() => setDraft(q), [q]);

  const query = useQuery({
    queryKey: ['search', q, page],
    queryFn: async (): Promise<DemoList> => {
      const response = await listDemos({ q: q === '' ? undefined : q, page, pageSize: PAGE_SIZE });
      if (response.status !== 200) throw new Error('搜索失败（' + response.status + '）');
      return response.data;
    },
    // 没有关键词时不打全量列表：搜索页的空态是提示，不是作品库的第二份。
    enabled: q !== '',
  });

  function submit(event: FormEvent) {
    event.preventDefault();
    const next = new URLSearchParams();
    if (draft.trim() !== '') next.set('q', draft.trim());
    setParams(next);
  }

  function goToPage(nextPage: number) {
    const next = new URLSearchParams(params);
    if (nextPage <= 1) next.delete('page');
    else next.set('page', String(nextPage));
    setParams(next);
  }

  return (
    <Page width="wide" title="搜索" description={q ? '关键词：' + q : '按标题或简介搜索公开作品。'}>
      <form className="mt-6 flex max-w-xl items-end gap-2" onSubmit={submit} role="search">
        <label className="flex flex-1 flex-col gap-1 text-sm">
          关键词
          <Input
            fullWidth
            type="search"
            value={draft}
            onChange={(event) => setDraft(event.target.value)}
            placeholder="标题或简介里的词"
            maxLength={100}
            aria-label="搜索作品"
          />
        </label>
        <Button type="submit" primary>
          搜索
        </Button>
      </form>

      {q === '' ? (
        <div className="mt-6">
          <EmptyState
            title="还没有搜索"
            description="输入关键词后按搜索。"
            action={
              <Link className="text-sm underline" to="/demos">
                去作品库
              </Link>
            }
          />
        </div>
      ) : null}

      {q !== '' && query.isPending ? (
        <div className="mt-6">
          <Skeleton lines={6} lineHeight="h-24" />
        </div>
      ) : null}

      {q !== '' && query.isError ? (
        <div className="mt-6">
          <ErrorState
            title="搜索失败"
            description={query.error instanceof Error ? query.error.message : '请稍后重试。'}
            onRetry={() => void query.refetch()}
            retrying={query.isFetching}
          />
        </div>
      ) : null}

      {q !== '' && query.data && query.data.items.length === 0 ? (
        <div className="mt-6">
          <EmptyState
            title="没有找到作品"
            description="换一个关键词，或者去作品库按分类筛选。"
            action={
              <Link className="text-sm underline" to="/demos">
                去作品库
              </Link>
            }
          />
        </div>
      ) : null}

      {query.data && query.data.items.length > 0 ? (
        <>
          <ul className="mt-6 grid list-none grid-cols-1 gap-4 p-0 sm:grid-cols-2 lg:grid-cols-3">
            {query.data.items.map((demo) => (
              <li key={demo.slug}>
                <Link
                  to={'/demos/' + demo.slug}
                  className="flex h-full flex-col rounded-xl border border-gray-200 bg-white p-4 text-inherit no-underline hover:border-gray-300"
                >
                  <h2 className="line-clamp-2 min-h-12 text-base font-medium text-gray-900">
                    {demo.title ?? '（无标题）'}
                  </h2>
                  <p className="mt-auto pt-3 text-xs text-gray-500">
                    {new Date(demo.createdAt).toLocaleDateString('zh-CN')}
                  </p>
                </Link>
              </li>
            ))}
          </ul>
          <div className="mt-4 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
            <span className="text-sm text-gray-500">共 {query.data.total} 个结果</span>
            <Pagination
              page={query.data.page}
              pageCount={Math.max(1, Math.ceil(query.data.total / query.data.pageSize))}
              onPageChange={goToPage}
              disabled={query.isFetching}
            />
          </div>
        </>
      ) : null}
    </Page>
  );
}
