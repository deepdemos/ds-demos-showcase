import { Link } from 'react-router';
import { useQuery } from '@tanstack/react-query';
import { Page, PageSection } from '@/components/layout/page';
import { Skeleton } from '@/components/ui';
import { listDemos, type DemoList } from '@/api';
import { DemoCard } from '@/features/demo/demo-card';
import { useAuth } from '@/features/auth/AuthContext';

// 首页只做一件事：把「这站是什么」和「现在能去哪」说清楚。
// 入口按登录态分叉——未登录给登录，已登录给投稿；两者都给的首页会把人劝走。
const actionLink =
  'inline-flex items-center rounded-lg border border-gray-300 px-4 py-2 text-sm hover:border-gray-400';

export function HomePage() {
  const { me } = useAuth();
  const latest = useQuery({
    queryKey: ['home-latest'],
    queryFn: async (): Promise<DemoList> => {
      const response = await listDemos({ sort: 'latest', page: 1, pageSize: 3 });
      if (response.status !== 200) throw new Error('加载失败（' + response.status + '）');
      return response.data;
    },
  });

  return (
    <Page
      title="DeepDemos 展示"
      description="这里托管可运行的作品：网页作品或压缩包，审核通过后公开。可以按形态、技术、模型筛选作品库，看排行榜；登录后能投稿和评分。"
    >
      <p className="mt-6 flex flex-wrap gap-3">
        <Link to="/demos" className={actionLink}>
          浏览作品库 →
        </Link>
        <Link to="/explore" className={actionLink}>
          探索
        </Link>
        <Link to="/rankings" className={actionLink}>
          排行榜
        </Link>
        <Link to="/models" className={actionLink}>
          模型目录
        </Link>
        {me ? (
          <Link to="/submit" className={actionLink}>
            投稿
          </Link>
        ) : (
          <Link to="/login" className={actionLink}>
            登录
          </Link>
        )}
      </p>

      <PageSection
        title="最新上架"
        description="最近发布的作品。"
        actions={
          <Link className="text-sm underline" to="/demos?sort=latest">
            查看全部
          </Link>
        }
      >
        {latest.isPending ? <Skeleton lines={3} /> : null}
        {latest.isError ? (
          <p className="m-0 text-sm text-gray-500">最新作品暂时读不到，先去作品库看看。</p>
        ) : null}
        {latest.data && latest.data.items.length === 0 ? (
          <p className="m-0 text-sm text-gray-500">还没有公开作品。</p>
        ) : null}
        {latest.data && latest.data.items.length > 0 ? (
          <ul className="grid list-none grid-cols-1 gap-4 p-0 sm:grid-cols-2 lg:grid-cols-3">
            {latest.data.items.map((demo) => (
              <li key={demo.slug}>
                <DemoCard demo={demo} />
              </li>
            ))}
          </ul>
        ) : null}
      </PageSection>
    </Page>
  );
}
