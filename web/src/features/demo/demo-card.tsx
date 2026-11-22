import { Link } from 'react-router';
import type { DemoSummary } from '@/api';

export type DemoCardData = Pick<
  DemoSummary,
  'slug' | 'title' | 'coverUrl' | 'createdAt' | 'matchedIn'
>;

/** 作品卡：作品库、搜索、探索共用一套结构与留白。 */
export function DemoCard({ demo }: { demo: DemoCardData }) {
  return (
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
      {demo.matchedIn === 'description' ? (
        <p className="mt-1 text-xs text-amber-700">简介命中</p>
      ) : null}
      <p className="mt-auto pt-3 text-xs text-gray-500">
        {new Date(demo.createdAt).toLocaleDateString('zh-CN')}
      </p>
    </Link>
  );
}
