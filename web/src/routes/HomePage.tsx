import { Link } from 'react-router';

export function HomePage() {
  return (
    <section className="mx-auto w-full max-w-3xl px-4 py-8">
      <h1 className="text-2xl font-semibold">DeepDemos 展示</h1>
      <p className="mt-2 text-sm text-gray-500 dark:text-gray-400">
        本站托管可运行的作品。目前开放的是作品库（只读）。开发环境可从右上角进入 Dev Room
        查看控件库。
      </p>
      <p className="mt-6">
        <Link
          to="/demos"
          className="inline-flex items-center rounded-lg border border-gray-300 px-4 py-2 text-sm hover:border-gray-400 dark:border-gray-700 dark:hover:border-gray-600"
        >
          浏览作品库 →
        </Link>
      </p>
    </section>
  );
}
