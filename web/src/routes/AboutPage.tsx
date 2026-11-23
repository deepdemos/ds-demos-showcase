import { Link } from 'react-router';
import { Page } from '@/components/layout/page';

export function AboutPage() {
  return (
    <Page
      title="关于"
      description="DeepDemos 展示托管可运行的 AI 作品：网页作品与压缩包，审核通过后公开。"
    >
      <h2 className="mt-6 text-lg font-semibold">作品从哪来</h2>
      <p className="mt-2 text-sm text-gray-700">
        作者在站内投稿，审核通过并发布后进入作品库。作品可以声明使用的模型；模型声明是作者自述，
        不是平台核验过的结论。
      </p>

      <h2 className="mt-6 text-lg font-semibold">数据与排序</h2>
      <p className="mt-2 text-sm text-gray-700">
        排行榜只统计当前已上架的作品；评分、浏览与下载都按站点记录计算。搜索只索引标题与简介，
        不读作品正文。
      </p>

      <h2 className="mt-6 text-lg font-semibold">去哪里</h2>
      <p className="mt-2 flex flex-wrap gap-3 text-sm">
        <Link className="underline" to="/demos">
          作品库
        </Link>
        <Link className="underline" to="/models">
          模型目录
        </Link>
        <Link className="underline" to="/community-guidelines">
          社区规范
        </Link>
      </p>
    </Page>
  );
}
