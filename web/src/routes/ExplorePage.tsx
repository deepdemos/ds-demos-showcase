import { Link } from 'react-router';
import { useQuery, type UseQueryResult } from '@tanstack/react-query';
import { EmptyState, ErrorState, Skeleton } from '@/components/ui';
import { Page, PageSection } from '@/components/layout/page';
import { DemoCard } from '@/features/demo/demo-card';
import { listDemos, type DemoList } from '@/api';

const SIZE = 6;

async function load(sort: 'hot' | 'latest'): Promise<DemoList> {
  const response = await listDemos({ sort, page: 1, pageSize: SIZE });
  if (response.status !== 200) throw new Error('加载失败（' + response.status + '）');
  return response.data;
}

function DemoSection({
  title,
  description,
  query,
  moreTo,
}: {
  title: string;
  description: string;
  query: UseQueryResult<DemoList>;
  moreTo: string;
}) {
  return (
    <PageSection
      title={title}
      description={description}
      actions={
        <Link className="text-sm underline" to={moreTo}>
          查看全部
        </Link>
      }
    >
      {query.isPending ? <Skeleton lines={3} /> : null}
      {query.isError ? (
        <ErrorState
          title="加载失败"
          description={query.error instanceof Error ? query.error.message : '请稍后重试。'}
          onRetry={() => void query.refetch()}
          retrying={query.isFetching}
        />
      ) : null}
      {query.data && query.data.items.length === 0 ? (
        <EmptyState title="还没有作品" description="等作品发布后会出现在这里。" />
      ) : null}
      {query.data && query.data.items.length > 0 ? (
        <ul className="grid list-none grid-cols-1 gap-4 p-0 sm:grid-cols-2 lg:grid-cols-3">
          {query.data.items.map((demo) => (
            <li key={demo.slug}>
              <DemoCard demo={demo} />
            </li>
          ))}
        </ul>
      ) : null}
    </PageSection>
  );
}

export function ExplorePage() {
  const hot = useQuery({ queryKey: ['explore', 'hot'], queryFn: () => load('hot') });
  const latest = useQuery({ queryKey: ['explore', 'latest'], queryFn: () => load('latest') });

  return (
    <Page
      width="wide"
      title="探索"
      description="从作品库之外换一个入口：看最近被浏览最多的作品，也看刚上架的作品。"
      actions={
        <div className="flex flex-wrap gap-3 text-sm">
          <Link className="underline" to="/models">
            模型目录
          </Link>
          <Link className="underline" to="/search">
            搜索作品
          </Link>
        </div>
      }
    >
      <DemoSection
        title="正在热门"
        description="按浏览、下载与人气综合排序；只统计已公开的作品。"
        query={hot}
        moreTo="/demos?sort=hot"
      />
      <DemoSection
        title="刚刚上架"
        description="最近发布的作品；发布时间从新到旧。"
        query={latest}
        moreTo="/demos?sort=latest"
      />
    </Page>
  );
}
