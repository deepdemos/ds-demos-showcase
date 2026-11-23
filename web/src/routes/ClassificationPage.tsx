import { useQuery } from '@tanstack/react-query';
import { Link, useParams, useSearchParams } from 'react-router';
import { Button, EmptyState, ErrorState, Pagination, Select, Skeleton } from '@/components/ui';
import { Page } from '@/components/layout/page';
import {
  listClassifications,
  listDemos,
  type Classification,
  type ClassificationCatalog,
  type ListDemosParams,
} from '@/api';

/**
 * 分类取值页（#456）：/classifications/:dimension/:slug。
 *
 * # 数据从哪来
 *
 * 页头与相邻取值来自 `GET /classifications`——它已经带每个取值的 `demoCount`
 * 与维度元数据（label / 顺序），不需要再加端点。作品列表复用
 * `GET /demos?<facet>=<slug>`：分类的筛选参数与维度同名（genre / technique /
 * subject），这是契约里写死的四个 facet 之三，不是前端猜的。
 *
 * # 计数为什么以列表的 total 为准
 *
 * 目录里的 demoCount 与列表同源（都按「已发布 + 有可见渠道」数），但目录可能
 * 是缓存的旧值。页头先显示目录计数，列表回来之后换成 total——两者不一致时，
 * 用户看到的是列表里真正有多少件。
 */

const PAGE_SIZE = 12;

const DIMENSIONS = ['genre', 'technique', 'subject'] as const;
type Dimension = (typeof DIMENSIONS)[number];

const SORT_OPTIONS = [
  { value: 'latest', label: '最新' },
  { value: 'hot', label: '热门' },
] as const;

function parseDimension(raw: string | undefined): Dimension | null {
  return DIMENSIONS.find((dimension) => dimension === raw) ?? null;
}

function parsePage(raw: string | null): number {
  const value = Number(raw);
  // 上限防止 (page-1)*pageSize 溢出后端 int32；越界页由空态给出路。
  return Number.isInteger(value) && value >= 1 ? Math.min(value, 100_000) : 1;
}

function facetParams(dimension: Dimension, slug: string): ListDemosParams {
  // facet 参数在契约里是可重复的数组（`?genre=a&genre=b`），单值也要包成数组。
  switch (dimension) {
    case 'genre':
      return { genre: [slug] };
    case 'technique':
      return { technique: [slug] };
    case 'subject':
      return { subject: [slug] };
  }
}

function findDimension(
  catalog: ClassificationCatalog,
  dimension: Dimension,
): ClassificationCatalog['dimensions'][number] | null {
  return catalog.dimensions.find((item) => item.dimension === dimension) ?? null;
}

function findValue(
  catalog: ClassificationCatalog,
  dimension: Dimension,
  slug: string,
): Classification | null {
  return catalog.items.find((item) => item.dimension === dimension && item.slug === slug) ?? null;
}

export function ClassificationPage() {
  const { dimension: rawDimension, slug = '' } = useParams();
  const [searchParams, setSearchParams] = useSearchParams();

  const dimension = parseDimension(rawDimension);
  const page = parsePage(searchParams.get('page'));
  const sort: ListDemosParams['sort'] = searchParams.get('sort') === 'hot' ? 'hot' : 'latest';

  const catalog = useQuery({
    queryKey: ['classifications'],
    queryFn: async () => {
      const response = await listClassifications();
      if (response.status !== 200) {
        throw new Error('分类目录加载失败（' + response.status + '）');
      }
      return response.data;
    },
    staleTime: 5 * 60_000,
  });

  const catalogData = catalog.data ?? null;
  const dimensionMeta = catalogData && dimension ? findDimension(catalogData, dimension) : null;
  const value = catalogData && dimension ? findValue(catalogData, dimension, slug) : null;

  // 目录已经答了「没有这一项」：不必再为它发一次列表请求。
  const missing = catalogData !== null && (dimension === null || !dimensionMeta || !value);

  const demos = useQuery({
    queryKey: ['classification', dimension, slug, page, sort],
    queryFn: async () => {
      const response = await listDemos({
        ...facetParams(dimension as Dimension, slug),
        page,
        pageSize: PAGE_SIZE,
        sort,
      });
      if (response.status !== 200) {
        throw new Error('作品列表加载失败（' + response.status + '）');
      }
      return response.data;
    },
    // 目录确认这一项存在之后才发列表请求：否则深链写错 slug 时也会白跑一次。
    enabled: catalogData !== null && !missing && dimension !== null && slug !== '',
  });

  const goToPage = (next: number) => {
    const params = new URLSearchParams(searchParams);
    if (next <= 1) params.delete('page');
    else params.set('page', String(next));
    setSearchParams(params, { replace: true });
  };

  const changeSort = (next: string) => {
    const params = new URLSearchParams(searchParams);
    if (next === 'latest') params.delete('sort');
    else params.set('sort', next);
    params.delete('page');
    setSearchParams(params, { replace: true });
  };

  if (catalog.isPending) {
    return (
      <Page title="分类">
        <Skeleton lines={4} />
      </Page>
    );
  }

  if ((catalog.isError && !catalogData) || missing) {
    return (
      <Page title="分类">
        {catalog.isError ? (
          <ErrorState
            title="分类目录加载失败"
            description="请稍后重试。"
            onRetry={() => void catalog.refetch()}
            retrying={catalog.isFetching}
          />
        ) : (
          <EmptyState
            title="这个分类不存在"
            description="可能是分类被合并或改名了。去作品库用筛选器找找看。"
            action={
              <Link className="text-sm underline" to="/demos">
                去作品库
              </Link>
            }
          />
        )}
      </Page>
    );
  }

  // 上面的 missing 分支已经返回；这里只为类型收窄，正常不会走到。
  if (dimension === null || !dimensionMeta || !value || !catalogData) return null;

  const total = demos.data?.total ?? value.demoCount;
  const siblings = catalogData.items.filter(
    (item) => item.dimension === dimension && item.slug !== slug,
  );

  return (
    <Page
      title={value.label}
      eyebrow={dimensionMeta.label + '分类'}
      description={'共 ' + total + ' 个作品'}
    >
      {value.description ? (
        <p className="mt-2 max-w-2xl text-sm text-gray-600">{value.description}</p>
      ) : null}

      <div className="mt-4 flex items-center justify-between gap-3">
        <span className="text-sm text-gray-500">筛选与作品库同源</span>
        <div className="w-32">
          <Select
            ariaLabel="排序"
            value={sort === 'hot' ? 'hot' : 'latest'}
            options={SORT_OPTIONS}
            onChange={changeSort}
          />
        </div>
      </div>

      {catalog.isError && catalogData ? (
        <p role="alert" className="mt-4 text-sm text-red-600">
          分类目录刷新失败，下面的内容仍可阅读。
          <Button type="button" className="ml-2" onClick={() => void catalog.refetch()}>
            重试
          </Button>
        </p>
      ) : null}

      {demos.isError ? (
        <div className="mt-6">
          <ErrorState
            title="作品列表加载失败"
            description="请稍后重试。"
            onRetry={() => void demos.refetch()}
            retrying={demos.isFetching}
          />
        </div>
      ) : null}

      {demos.isPending ? (
        <div className="mt-6">
          <Skeleton lines={4} lineHeight="h-24" />
        </div>
      ) : null}

      {demos.data && demos.data.items.length === 0 ? (
        <div className="mt-6">
          {demos.data.total > 0 && page > 1 ? (
            <EmptyState
              title="这一页没有作品"
              description={'共 ' + demos.data.total + ' 个作品，这个页码已经越界。'}
              action={
                <Link
                  className="text-sm underline"
                  to={
                    '/classifications/' +
                    dimension +
                    '/' +
                    encodeURIComponent(slug) +
                    (sort === 'hot' ? '?sort=hot' : '')
                  }
                >
                  回到第 1 页
                </Link>
              }
            />
          ) : (
            <EmptyState
              title="这个分类下还没有作品"
              description="等有作品挂上这个分类并通过审核，它就会出现在这里。"
            />
          )}
        </div>
      ) : null}

      {demos.data && demos.data.items.length > 0 ? (
        <>
          <ul className="mt-6 m-0 grid list-none grid-cols-1 gap-4 p-0 sm:grid-cols-2 lg:grid-cols-3">
            {demos.data.items.map((demo) => (
              <li key={demo.slug}>
                <Link
                  to={'/demos/' + demo.slug}
                  className="flex h-full flex-col rounded-xl border border-gray-200 bg-white p-4 text-inherit no-underline hover:border-gray-300"
                >
                  <div className="mb-3 flex aspect-video items-center justify-center overflow-hidden rounded-lg bg-gray-100 text-xs text-gray-400">
                    {demo.coverUrl ? (
                      <img src={demo.coverUrl} alt="" className="h-full w-full object-cover" />
                    ) : (
                      '无封面'
                    )}
                  </div>
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

          <div className="mt-4 flex items-center justify-between">
            <span className="text-sm text-gray-500">共 {total} 个作品</span>
            <Pagination
              page={demos.data.page}
              pageCount={Math.max(1, Math.ceil(demos.data.total / demos.data.pageSize))}
              onPageChange={goToPage}
              disabled={demos.isFetching}
            />
          </div>
        </>
      ) : null}

      {siblings.length > 0 ? (
        <section className="mt-10 space-y-2">
          <h2 className="text-sm font-medium text-gray-600">
            同一{dimensionMeta.label}下的其他分类
          </h2>
          <ul className="flex list-none flex-wrap gap-2 p-0">
            {siblings.map((item) => (
              <li key={item.slug}>
                <Link
                  to={'/classifications/' + item.dimension + '/' + encodeURIComponent(item.slug)}
                  className="inline-flex items-center gap-1 rounded-full border border-gray-200 px-3 py-1 text-xs text-gray-700 no-underline hover:border-gray-300"
                >
                  {item.label}
                  <span className="text-gray-400">{item.demoCount}</span>
                </Link>
              </li>
            ))}
          </ul>
        </section>
      ) : null}
    </Page>
  );
}
