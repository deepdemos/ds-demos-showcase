import { useQuery } from '@tanstack/react-query';
import { Link, useParams, useSearchParams } from 'react-router';
import { EmptyState, ErrorState, Pagination, Skeleton } from '@/components/ui';
import { getUserProfile, listUserDemos } from '@/api';

/**
 * 作者主页（#425）：/u/:username。
 *
 * # 只做「公开资料 + 公开作品」
 *
 * issue 里的四个区块有两个没有数据源：社区贡献依赖关注/获赞，公开收藏夹依赖收藏域。
 * 这里不拿空区块占位——一个写着「暂无」的区块和「没有这项功能」是两种事实，
 * 混在一起会让读者以为作者真的没有贡献/收藏。等数据源落地再补区块。
 *
 * # 404 与空列表
 *
 * 用户名不存在（或账号非 active）时资料端点回 404，页面落「没有这个作者」；
 * 作者存在但没有公开作品时列表回 200 + 空数组，页面落「还没有公开作品」。
 * 两者不能混：前者是地址错了，后者是作者还没发东西。
 */

const PAGE_SIZE = 12;

function parsePage(raw: string | null): number {
  const value = Number(raw);
  return Number.isInteger(value) && value >= 1 ? value : 1;
}

export function AuthorPage() {
  const { username = '' } = useParams();
  const [searchParams, setSearchParams] = useSearchParams();
  const page = parsePage(searchParams.get('page'));

  const profile = useQuery({
    queryKey: ['author', username],
    queryFn: async () => {
      const response = await getUserProfile(username);
      if (response.status === 404) return null;
      if (response.status !== 200) {
        throw new Error('作者资料加载失败（' + response.status + '）');
      }
      return response.data;
    },
    enabled: username !== '',
  });

  const demos = useQuery({
    queryKey: ['author-demos', username, page],
    queryFn: async () => {
      const response = await listUserDemos(username, { page, pageSize: PAGE_SIZE });
      if (response.status !== 200) {
        throw new Error('作品列表加载失败（' + response.status + '）');
      }
      return response.data;
    },
    enabled: profile.data !== null && profile.data !== undefined,
  });

  const goToPage = (next: number) => {
    const params = new URLSearchParams(searchParams);
    if (next <= 1) params.delete('page');
    else params.set('page', String(next));
    setSearchParams(params, { replace: true });
  };

  if (profile.isPending) {
    return (
      <section className="mx-auto w-full max-w-5xl px-4 py-8">
        <Skeleton lines={4} />
      </section>
    );
  }

  if (profile.isError) {
    return (
      <section className="mx-auto w-full max-w-5xl px-4 py-8">
        <ErrorState
          title="作者资料加载失败"
          description="请稍后重试。"
          onRetry={() => void profile.refetch()}
          retrying={profile.isFetching}
        />
      </section>
    );
  }

  if (profile.data === null) {
    return (
      <section className="mx-auto w-full max-w-5xl px-4 py-8">
        <EmptyState
          title="没有这个作者"
          description="用户名不存在，或者这个账号已经不可见。"
          action={
            <Link className="text-sm underline" to="/demos">
              去作品库
            </Link>
          }
        />
      </section>
    );
  }

  const author = profile.data;
  const total = demos.data?.total ?? author.demoCount;

  return (
    <section className="mx-auto w-full max-w-5xl px-4 py-8">
      <header className="flex flex-wrap items-center gap-4">
        {author.avatarUrl ? (
          <img
            src={author.avatarUrl}
            alt=""
            className="h-16 w-16 flex-none rounded-full object-cover"
          />
        ) : (
          <div
            aria-hidden="true"
            className="h-16 w-16 flex-none rounded-full bg-gray-100 dark:bg-gray-800"
          />
        )}
        <div className="min-w-0">
          <h1 className="truncate text-2xl font-semibold text-gray-900 dark:text-gray-100">
            {author.displayName || author.username}
          </h1>
          <p className="text-sm text-gray-500 dark:text-gray-400">@{author.username}</p>
        </div>
      </header>

      {author.bio ? (
        <p className="mt-4 max-w-3xl whitespace-pre-line text-sm leading-6 text-gray-700 dark:text-gray-300">
          {author.bio}
        </p>
      ) : null}

      <p className="mt-3 text-sm text-gray-500 dark:text-gray-400">
        加入于 {new Date(author.joinedAt).toLocaleDateString('zh-CN')} · 公开作品 {total} 件
      </p>

      <section className="mt-8 space-y-3">
        <h2 className="text-lg font-medium text-gray-900 dark:text-gray-100">公开作品</h2>

        {demos.isError ? (
          <ErrorState
            title="作品列表加载失败"
            description="请稍后重试。"
            onRetry={() => void demos.refetch()}
            retrying={demos.isFetching}
          />
        ) : null}

        {demos.isPending ? <Skeleton lines={3} lineHeight="h-24" /> : null}

        {demos.data && demos.data.items.length === 0 ? (
          <EmptyState title="还没有公开作品" description="作者发布并通过审核的作品会出现在这里。" />
        ) : null}

        {demos.data && demos.data.items.length > 0 ? (
          <>
            <ul className="m-0 grid list-none grid-cols-1 gap-4 p-0 sm:grid-cols-2 lg:grid-cols-3">
              {demos.data.items.map((demo) => (
                <li key={demo.slug}>
                  <Link
                    to={'/demos/' + demo.slug}
                    className="flex h-full flex-col rounded-xl border border-gray-200 bg-white p-4 text-inherit no-underline hover:border-gray-300 dark:border-gray-800 dark:bg-gray-900 dark:hover:border-gray-700"
                  >
                    <div className="mb-3 flex aspect-video items-center justify-center overflow-hidden rounded-lg bg-gray-100 text-xs text-gray-400 dark:bg-gray-800 dark:text-gray-500">
                      {demo.coverUrl ? (
                        <img src={demo.coverUrl} alt="" className="h-full w-full object-cover" />
                      ) : (
                        '无封面'
                      )}
                    </div>
                    <h3 className="line-clamp-2 min-h-12 text-base font-medium text-gray-900 dark:text-gray-100">
                      {demo.title ?? '（无标题）'}
                    </h3>
                    <p className="mt-auto pt-3 text-xs text-gray-500 dark:text-gray-400">
                      {new Date(demo.createdAt).toLocaleDateString('zh-CN')}
                    </p>
                  </Link>
                </li>
              ))}
            </ul>

            <div className="mt-4 flex items-center justify-between">
              <span className="text-sm text-gray-500 dark:text-gray-400">共 {total} 个作品</span>
              <Pagination
                page={demos.data.page}
                pageCount={Math.max(1, Math.ceil(demos.data.total / demos.data.pageSize))}
                onPageChange={goToPage}
                disabled={demos.isFetching}
              />
            </div>
          </>
        ) : null}
      </section>
    </section>
  );
}
