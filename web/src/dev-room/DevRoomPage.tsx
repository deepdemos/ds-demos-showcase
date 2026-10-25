import { useEffect, useMemo, useState } from 'react';
import { useNavigate, useSearchParams } from 'react-router';
import { Button, ThemeToggle, type Theme } from '@/components/ui';
import { controlCategories, controls, type ControlCategoryId } from '@/components/ui/registry';

const PAGE_SIZE = 6;
const THEME_KEY = 'dev-room-theme';

function readTheme(): Theme {
  try {
    return localStorage.getItem(THEME_KEY) === 'dark' ? 'dark' : 'light';
  } catch {
    return 'light';
  }
}

export function DevRoomPage() {
  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();
  const [theme, setTheme] = useState<Theme>(readTheme);

  useEffect(() => {
    document.documentElement.classList.toggle('ring-ui-theme-dark', theme === 'dark');
    try {
      localStorage.setItem(THEME_KEY, theme);
    } catch {
      // 隐私模式下 localStorage 可能不可用，忽略即可。
    }
    return () => document.documentElement.classList.remove('ring-ui-theme-dark');
  }, [theme]);

  const categoryParam = searchParams.get('category');
  const activeCategory: ControlCategoryId | 'all' = controlCategories.some(
    (category) => category.id === categoryParam,
  )
    ? (categoryParam as ControlCategoryId)
    : 'all';

  const filteredControls = useMemo(
    () =>
      activeCategory === 'all'
        ? controls
        : controls.filter((control) => control.category === activeCategory),
    [activeCategory],
  );

  const totalPages = Math.max(1, Math.ceil(filteredControls.length / PAGE_SIZE));
  const requestedPage = Number.parseInt(searchParams.get('page') ?? '1', 10);
  const page = Math.min(
    Math.max(Number.isFinite(requestedPage) ? requestedPage : 1, 1),
    totalPages,
  );
  const visibleControls = filteredControls.slice((page - 1) * PAGE_SIZE, page * PAGE_SIZE);

  function go(nextCategory: ControlCategoryId | 'all', nextPage: number) {
    setSearchParams({ category: nextCategory, page: String(nextPage) });
  }

  return (
    <div className="min-h-screen bg-neutral-100 font-sans text-neutral-900 dark:bg-neutral-950 dark:text-neutral-100">
      <div className="mx-auto w-full max-w-6xl px-4 py-8 sm:px-6">
        <header className="flex flex-wrap items-start justify-between gap-4 border-b border-neutral-200 pb-6 dark:border-neutral-800">
          <div>
            <h1 className="text-2xl font-semibold">组件开发室</h1>
            <p className="mt-2 max-w-2xl text-sm text-neutral-500 dark:text-neutral-400">
              控件库的登记与预览页。每个控件用「控件名/样式」作为注册名，可按分类筛选、分页查看。
            </p>
          </div>
          <div className="flex items-center gap-2">
            <Button onClick={() => navigate('/')}>返回首页</Button>
            <ThemeToggle
              theme={theme}
              onToggle={() => setTheme((current) => (current === 'dark' ? 'light' : 'dark'))}
            />
          </div>
        </header>

        <div className="mt-6 flex flex-wrap items-center gap-2">
          <Button active={activeCategory === 'all'} onClick={() => go('all', 1)}>
            全部
          </Button>
          {controlCategories.map((category) => (
            <Button
              key={category.id}
              active={activeCategory === category.id}
              onClick={() => go(category.id, 1)}
            >
              {category.name}
            </Button>
          ))}
          <span className="ml-auto text-sm text-neutral-500 dark:text-neutral-400">
            共 {filteredControls.length} 个控件
          </span>
        </div>

        <div className="mt-6 grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3">
          {visibleControls.map(({ id, name, description, Demo }) => (
            <article
              key={id}
              data-control-id={id}
              className="flex flex-col rounded-xl border border-neutral-200 bg-white p-5 shadow-sm dark:border-neutral-800 dark:bg-neutral-900"
            >
              <div className="flex items-start justify-between gap-3">
                <h2 className="text-base font-semibold">{name}</h2>
                <code className="rounded bg-neutral-100 px-2 py-0.5 text-xs text-neutral-500 dark:bg-neutral-800 dark:text-neutral-400">
                  #{id}
                </code>
              </div>
              <p className="mt-1 text-sm text-neutral-500 dark:text-neutral-400">{description}</p>
              <div className="mt-4 flex min-h-20 items-center justify-center rounded-lg border border-dashed border-neutral-200 bg-neutral-50 p-4 dark:border-neutral-800 dark:bg-neutral-950/40">
                <Demo />
              </div>
            </article>
          ))}
        </div>

        <footer className="mt-8 flex flex-wrap items-center justify-between gap-3 border-t border-neutral-200 pt-4 dark:border-neutral-800">
          <span className="text-sm text-neutral-500 dark:text-neutral-400">
            第 {page} / {totalPages} 页
          </span>
          <div className="flex gap-2">
            <Button disabled={page <= 1} onClick={() => go(activeCategory, page - 1)}>
              上一页
            </Button>
            <Button disabled={page >= totalPages} onClick={() => go(activeCategory, page + 1)}>
              下一页
            </Button>
          </div>
        </footer>
      </div>
    </div>
  );
}
