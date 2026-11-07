import { useEffect, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Link, useSearchParams } from 'react-router';
import {
  Button,
  Checkbox,
  EmptyState,
  ErrorState,
  Input,
  Pagination,
  Skeleton,
} from '@/components/ui';
import {
  listClassifications,
  listDemos,
  listModels,
  type ClassificationCatalog,
  type DemoList,
  type ListDemosParams,
  type Model,
} from '@/api';

const PAGE_SIZE = 12;

/** 四个可筛维度。顺序与契约里的参数顺序一致。 */
const FACETS = ['genre', 'technique', 'subject', 'model'] as const;
type Facet = (typeof FACETS)[number];

/**
 * 从 URL 读一个维度的选中值。
 *
 * 去重是必须的：用户可以手改 URL 写出 `?genre=game&genre=game`，不去重会让
 * 「已选 1」显示成 2，也让请求里的数组带重复项。
 */
function readFacet(params: URLSearchParams, facet: Facet): string[] {
  const raw = params.getAll(facet).flatMap((v) => v.split(','));
  return [...new Set(raw.map((v) => v.trim()).filter((v) => v !== ''))];
}

async function fetchCatalog(): Promise<ClassificationCatalog> {
  const response = await listClassifications();
  if (response.status === 200) return response.data;
  throw new Error(`筛选项目录加载失败（HTTP ${response.status}）`);
}

/** 模型筛选的候选：只取 active——候选 / 退役模型不该出现在筛选器里。 */
async function fetchModels(): Promise<Model[]> {
  const response = await listModels({ status: 'active' });
  if (response.status === 200) return response.data.items;
  throw new Error(`模型目录加载失败（HTTP ${response.status}）`);
}

/** 页码只认正整数，其余一律回第 1 页——URL 是用户能改的，不能假设它干净。 */
function readPage(raw: string | null): number {
  const n = Number(raw);
  return Number.isInteger(n) && n >= 1 ? n : 1;
}

async function fetchDemos(
  page: number,
  q: string,
  facets: Record<Facet, string[]>,
): Promise<DemoList> {
  const picked: Pick<ListDemosParams, Facet> = {};
  for (const facet of FACETS) {
    // 空数组不传：与 q 同理，空值进请求只会让缓存键多分一份。
    if (facets[facet].length > 0) picked[facet] = facets[facet];
  }
  const response = await listDemos({
    page,
    pageSize: PAGE_SIZE,
    sort: 'latest',
    // 空串不传：URL 里带一个空 q 会让缓存键多分出一份，也让「清空搜索」看起来没生效。
    ...(q ? { q } : {}),
    ...picked,
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
  // 维度筛选条件。每次渲染从 URL 重算，不单独存 state——URL 是唯一事实源，
  // 否则「后退」之后勾选框会与地址栏说的不是一回事。
  const facets: Record<Facet, string[]> = {
    genre: readFacet(searchParams, 'genre'),
    technique: readFacet(searchParams, 'technique'),
    subject: readFacet(searchParams, 'subject'),
    model: readFacet(searchParams, 'model'),
  };
  const activeFacets = FACETS.reduce((n, f) => n + facets[f].length, 0);

  // URL 变了（后退、点链接）就把输入框同步过来；否则输入框会停在旧词上，
  // 看起来像「搜索没生效」。
  useEffect(() => {
    setDraft(q);
  }, [q]);

  const { data, isPending, isError, error, refetch, isFetching } = useQuery({
    // 缓存键里带上筛选条件：不带的话，切换筛选会先显示上一组结果的缓存
    // （看起来像「点了没反应」），再闪成新结果。
    queryKey: ['demos', page, q, facets.genre, facets.technique, facets.subject, facets.model],
    queryFn: () => fetchDemos(page, q, facets),
  });

  const catalog = useQuery({
    queryKey: ['classifications'],
    queryFn: fetchCatalog,
    staleTime: 5 * 60 * 1000,
  });

  const models = useQuery({
    queryKey: ['models'],
    queryFn: fetchModels,
    staleTime: 5 * 60 * 1000,
  });

  const toggleFacet = (facet: Facet, slug: string, checked: boolean) => {
    const params = new URLSearchParams(searchParams);
    const next = checked ? [...facets[facet], slug] : facets[facet].filter((s) => s !== slug);
    params.delete(facet);
    for (const value of next) params.append(facet, value);
    // 换筛选必须回第 1 页：停在第 3 页会得到一片空白，看起来像筛不到。
    params.delete('page');
    setSearchParams(params);
  };

  const clearFacets = () => {
    const params = new URLSearchParams(searchParams);
    for (const facet of FACETS) params.delete(facet);
    params.delete('page');
    setSearchParams(params);
  };

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

        {/*
          筛选器只在目录读到之后出现：目录没来时给出一个空壳会让人以为「一个都筛不了」。
          只列「至少有一件作品在用」（demoCount > 0）或「当前已选中」的词条——
          92 条里大半是 0，全列出来会把筛选器变成一面墙。
        */}
        {catalog.data ? (
          <details className="mt-4 max-w-xl" open={activeFacets > 0}>
            <summary className="cursor-pointer text-sm text-gray-600 dark:text-gray-300">
              筛选{activeFacets > 0 ? `（已选 ${activeFacets} 项）` : ''}
              {activeFacets > 0 ? (
                <span className="ml-3">
                  <Button type="button" inline onClick={clearFacets}>
                    清除筛选
                  </Button>
                </span>
              ) : null}
            </summary>

            <div className="mt-3 flex flex-col gap-3">
              {catalog.data.dimensions.map((dim) => {
                const options = catalog.data.items.filter(
                  (c) =>
                    c.dimension === dim.dimension &&
                    (c.demoCount > 0 || facets[dim.dimension].includes(c.slug)),
                );
                if (options.length === 0) return null;
                return (
                  <div key={dim.dimension} className="flex flex-col gap-1">
                    <p className="m-0 text-xs text-gray-600 dark:text-gray-400">{dim.label}</p>
                    <div className="flex flex-wrap gap-x-4 gap-y-1">
                      {options.map((c) => (
                        <Checkbox
                          key={`${c.dimension}:${c.slug}`}
                          label={`${c.label}（${c.demoCount}）`}
                          checked={facets[dim.dimension].includes(c.slug)}
                          indeterminate={false}
                          disabled={isFetching}
                          onChange={(e) => toggleFacet(dim.dimension, c.slug, e.target.checked)}
                        />
                      ))}
                    </div>
                  </div>
                );
              })}

              {/* 模型维度不在分类目录里：它来自 models 表，与分类是两套数据。 */}
              {models.data && models.data.length > 0 ? (
                <div className="flex flex-col gap-1">
                  <p className="m-0 text-xs text-gray-600 dark:text-gray-400">模型</p>
                  <div className="flex flex-wrap gap-x-4 gap-y-1">
                    {models.data.map((m) => (
                      <Checkbox
                        key={m.slug}
                        label={m.name}
                        checked={facets.model.includes(m.slug)}
                        indeterminate={false}
                        disabled={isFetching}
                        onChange={(e) => toggleFacet('model', m.slug, e.target.checked)}
                      />
                    ))}
                  </div>
                </div>
              ) : null}
            </div>
          </details>
        ) : null}
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
