import { useMemo, useState } from 'react';
import { Link } from 'react-router';
import { Page } from '@/components/layout/page';
import { Input, Select } from '@/components/ui';
import {
  benchmarkLeaderboards,
  boardCategoryLabel,
  boardCategoryOrder,
  boardMatchesFreshness,
  freshCutoff,
  type BoardCategory,
  type BoardEntry,
  type Freshness,
} from '@/content/benchmark-leaderboards';

/**
 * 排行榜（`/benchmarks/leaderboards`）。
 *
 * 目录页：每张榜一张卡片（名字、一句话说明、前几名预览），按能力分组。
 * 内容来自 `src/content/benchmark-leaderboards.json`：改文案改 JSON、走 PR。
 *
 * 三条口径：
 * - 默认只看近 6 个月有数据的榜；旧榜切「全部」才出现。
 * - 预览取每个模型在该榜的最高分，不跨榜比较。
 * - 卡片外链到 Epoch 原榜；站内单榜详情要等导入后才有 slug。
 */

const EPOCH_URL = 'https://epoch.ai/benchmarks';

const categoryOptions = [
  { value: 'all', label: '全部类别' },
  ...boardCategoryOrder.map((category) => ({
    value: category,
    label: boardCategoryLabel[category],
  })),
];

const sortOptions = [
  { value: 'latest', label: '按最新数据' },
  { value: 'models', label: '按模型数' },
  { value: 'name', label: '按名称' },
];

const freshOptions = [
  { value: 'recent', label: '近 6 个月' },
  { value: 'y2026', label: '今年' },
  { value: 'all', label: '全部（含旧榜）' },
];

const densityOptions = [
  { value: 'standard', label: '标准卡片' },
  { value: 'compact', label: '紧凑卡片' },
];

const previewOptions = [
  { value: '5', label: '预览前 5' },
  { value: '3', label: '预览前 3' },
];

function sortBoards(boards: BoardEntry[], sort: string): BoardEntry[] {
  const sorted = [...boards];
  if (sort === 'models') {
    sorted.sort((a, b) => b.models - a.models);
  } else if (sort === 'name') {
    sorted.sort((a, b) => a.name.localeCompare(b.name, 'zh-CN'));
  } else {
    sorted.sort((a, b) => (b.latest ?? '').localeCompare(a.latest ?? ''));
  }
  return sorted;
}

function BoardCard({
  board,
  compact,
  preview,
}: {
  board: BoardEntry;
  compact: boolean;
  preview: number;
}) {
  const max = Math.max(...board.top.map((entry) => entry.value), 1);
  const rows = board.top.slice(0, preview);

  return (
    <a
      href={EPOCH_URL}
      target="_blank"
      rel="noreferrer noopener"
      className={
        'flex flex-col rounded-lg border border-gray-200 bg-white p-4 text-gray-900 no-underline transition-colors hover:border-gray-400 ' +
        (compact ? 'gap-2' : 'gap-3')
      }
    >
      <h3 className="m-0 text-sm font-semibold">{board.name}</h3>
      <p className="m-0 text-xs text-gray-600">{board.description}</p>
      <div className="flex flex-col gap-1" aria-hidden="true">
        {rows.map((entry) => (
          <div key={entry.model} className="grid grid-cols-[7rem_1fr_2.5rem] items-center gap-2">
            <span className="truncate text-xs text-gray-500">{entry.model}</span>
            <span className="h-1.5 overflow-hidden rounded bg-gray-100">
              <span
                className="block h-full rounded bg-gray-500/60"
                style={{ width: Math.max(2, (entry.value / max) * 100) + '%' }}
              />
            </span>
            <span className="text-right text-xs tabular-nums text-gray-500">{entry.value}</span>
          </div>
        ))}
      </div>
      <div className="mt-auto flex items-center justify-between border-t border-gray-100 pt-2 text-xs text-gray-500">
        <span>
          {board.models} 个模型
          {board.latest ? ' · 最新 ' + board.latest.slice(0, 7) : ''}
        </span>
        <span className="text-gray-700">原榜 →</span>
      </div>
    </a>
  );
}

export function BenchmarkLeaderboardsPage() {
  const [query, setQuery] = useState('');
  const [category, setCategory] = useState('all');
  const [sort, setSort] = useState('latest');
  const [density, setDensity] = useState('standard');
  const [fresh, setFresh] = useState<Freshness>('recent');
  const [preview, setPreview] = useState('5');

  const cutoff = freshCutoff(benchmarkLeaderboards.updatedAt);
  const yearStart = benchmarkLeaderboards.updatedAt.slice(0, 4) + '-01-01';

  const sections = useMemo(() => {
    const keyword = query.trim().toLowerCase();
    const boards = benchmarkLeaderboards.boards.filter((board) => {
      if (!boardMatchesFreshness(board.latest, fresh, cutoff, yearStart)) return false;
      if (category !== 'all' && board.category !== category) return false;
      if (!keyword) return true;
      return (board.name + ' ' + board.description).toLowerCase().includes(keyword);
    });

    return boardCategoryOrder
      .map((key: BoardCategory) => ({
        key,
        label: boardCategoryLabel[key],
        boards: sortBoards(
          boards.filter((board) => board.category === key),
          sort,
        ),
      }))
      .filter((section) => section.boards.length > 0);
  }, [query, category, sort, fresh, cutoff, yearStart]);

  const shown = sections.reduce((sum, section) => sum + section.boards.length, 0);

  return (
    <Page title={benchmarkLeaderboards.title} description={benchmarkLeaderboards.intro}>
      <div className="mt-6 flex flex-col gap-8">
        <div className="flex flex-wrap items-end gap-3">
          <div className="min-w-[16rem] flex-1">
            <Input
              fullWidth
              placeholder="搜榜名或描述…"
              aria-label="搜榜名或描述"
              value={query}
              onChange={(event) => setQuery(event.target.value)}
            />
          </div>
          <Select
            ariaLabel="类别"
            value={category}
            options={categoryOptions}
            onChange={setCategory}
          />
          <Select ariaLabel="排序" value={sort} options={sortOptions} onChange={setSort} />
          <Select
            ariaLabel="新鲜度"
            value={fresh}
            options={freshOptions}
            onChange={(value) => setFresh(value as Freshness)}
          />
          <Select
            ariaLabel="卡片密度"
            value={density}
            options={densityOptions}
            onChange={setDensity}
          />
          <Select
            ariaLabel="预览条数"
            value={preview}
            options={previewOptions}
            onChange={setPreview}
          />
        </div>

        <p className="m-0 text-xs text-gray-500">
          {shown} / {benchmarkLeaderboards.boards.length} 张榜 · 默认只看近 6 个月有数据的榜
        </p>

        {sections.length === 0 ? (
          <p className="m-0 text-sm text-gray-600">
            没有匹配的榜，换个关键词、类别或「全部（含旧榜）」试试。
          </p>
        ) : (
          sections.map((section) => (
            <section key={section.key}>
              <h2 className="m-0 text-base font-semibold text-gray-900">
                {section.label}
                <span className="ml-2 text-sm font-normal text-gray-500">
                  {section.boards.length}
                </span>
              </h2>
              <div
                className={
                  'mt-3 grid gap-3 ' +
                  (density === 'compact'
                    ? 'grid-cols-1 sm:grid-cols-2 lg:grid-cols-3'
                    : 'grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4')
                }
              >
                {section.boards.map((board) => (
                  <BoardCard
                    key={board.id}
                    board={board}
                    compact={density === 'compact'}
                    preview={Number(preview)}
                  />
                ))}
              </div>
            </section>
          ))
        )}

        <p className="m-0 text-xs text-gray-500">
          数据：{benchmarkLeaderboards.source} · 内容更新 {benchmarkLeaderboards.updatedAt}
          。预览取每个模型在该榜的最高分，不跨榜比较。
        </p>

        <p className="m-0 text-sm">
          <Link to="/benchmarks" className="underline-offset-2 hover:underline">
            回基准目录
          </Link>
        </p>
      </div>
    </Page>
  );
}
