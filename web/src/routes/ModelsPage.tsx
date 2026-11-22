import { useEffect, useMemo, useState, type FormEvent } from 'react';
import { Link, useSearchParams } from 'react-router';
import { useQuery } from '@tanstack/react-query';
import { Button, EmptyState, ErrorState, Input, Pagination, Skeleton } from '@/components/ui';
import { Page } from '@/components/layout/page';
import { listModels, type Model } from '@/api';

const PAGE_SIZE = 50;

function matches(model: Model, q: string): boolean {
  if (q === '') return true;
  const needle = q.toLowerCase();
  return (
    model.name.toLowerCase().includes(needle) ||
    model.slug.toLowerCase().includes(needle) ||
    model.vendor.toLowerCase().includes(needle)
  );
}

export function ModelsPage() {
  const [params, setParams] = useSearchParams();
  const q = (params.get('q') ?? '').trim();
  const page = Math.max(1, Number(params.get('page')) || 1);
  const [draft, setDraft] = useState(q);

  useEffect(() => setDraft(q), [q]);

  const query = useQuery({
    queryKey: ['models', 'active'],
    queryFn: async (): Promise<Model[]> => {
      const response = await listModels({ status: 'active' });
      if (response.status !== 200) throw new Error('模型目录加载失败（' + response.status + '）');
      return response.data.items;
    },
  });

  const filtered = useMemo(
    () => (query.data ?? []).filter((model) => matches(model, q)),
    [query.data, q],
  );
  const pageCount = Math.max(1, Math.ceil(filtered.length / PAGE_SIZE));
  const current = Math.min(page, pageCount);
  const visible = filtered.slice((current - 1) * PAGE_SIZE, current * PAGE_SIZE);

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
    setParams(next, { replace: true });
  }

  return (
    <Page
      width="wide"
      title="模型目录"
      description="站内登记的模型档案：厂商、状态与引用它的作品。按名字排序。"
    >
      <form className="mt-6 flex max-w-xl items-end gap-2" onSubmit={submit} role="search">
        <label className="flex flex-1 flex-col gap-1 text-sm">
          关键词
          <Input
            fullWidth
            type="search"
            value={draft}
            onChange={(event) => setDraft(event.target.value)}
            placeholder="模型名、slug 或厂商"
            maxLength={100}
            aria-label="搜索模型"
          />
        </label>
        <Button type="submit" primary>
          搜索
        </Button>
        {q !== '' ? (
          <Button
            type="button"
            onClick={() => {
              setDraft('');
              setParams(new URLSearchParams());
            }}
          >
            清除
          </Button>
        ) : null}
      </form>

      {query.isPending ? (
        <div className="mt-6">
          <Skeleton lines={6} lineHeight="h-12" />
        </div>
      ) : null}

      {query.isError ? (
        <div className="mt-6">
          <ErrorState
            title="模型目录加载失败"
            description={query.error instanceof Error ? query.error.message : '请稍后重试。'}
            onRetry={() => void query.refetch()}
            retrying={query.isFetching}
          />
        </div>
      ) : null}

      {query.data && filtered.length === 0 ? (
        <div className="mt-6">
          <EmptyState
            title={q ? '没有匹配的模型' : '模型目录还是空的'}
            description={q ? '换一个关键词试试。' : '模型目录同步后会出现在这里。'}
          />
        </div>
      ) : null}

      {visible.length > 0 ? (
        <>
          <ul className="mt-6 list-none divide-y divide-gray-200 border-y border-gray-200 p-0">
            {visible.map((model) => (
              <li
                key={model.slug}
                className="flex flex-wrap items-center justify-between gap-2 py-3"
              >
                <div className="min-w-0">
                  <Link
                    className="font-medium text-gray-900 underline-offset-2 hover:underline"
                    to={'/models/' + model.slug}
                  >
                    {model.name}
                  </Link>
                  <p className="m-0 mt-0.5 text-xs text-gray-500">
                    {model.vendor || '厂商未知'} · {model.slug}
                  </p>
                </div>
                <Link
                  className="text-sm underline"
                  to={'/demos?model=' + encodeURIComponent(model.slug)}
                >
                  相关作品
                </Link>
              </li>
            ))}
          </ul>
          <div className="mt-4 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
            <span className="text-sm text-gray-500">共 {filtered.length} 个模型</span>
            <Pagination
              page={current}
              pageCount={pageCount}
              onPageChange={goToPage}
              disabled={query.isFetching}
            />
          </div>
        </>
      ) : null}
    </Page>
  );
}
