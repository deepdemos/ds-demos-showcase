import content from './benchmark-sources.json';
import type { DisplayTier } from './benchmark-tier';

/**
 * 基准来源（上游榜单）的内容与类型。
 *
 * 这不是「我们收录的基准」—— 那份数据在 `benchmarks` 表里，走 API。
 * 这里是一份静态内容：业界上游榜单的速查表。改文案改 JSON、走 PR，
 * 不建后台、不接 CMS（#568 决议）。
 */

export type BenchmarkCategory =
  'coding' | 'agent' | 'general' | 'math' | 'multimodal' | 'safety' | 'other';

export interface SourceColumn {
  key: string;
  label: string;
  explain: string;
}

/** 档位说明只有解释；档位名在 `benchmark-tier.ts`，不在这里写第二遍。 */
export interface TierLegendItem {
  value: DisplayTier;
  explain: string;
}

export interface BenchmarkSource {
  id: string;
  name: string;
  publisher: string;
  url: string;
  /** 命中 `benchmarks.slug` 时，档位以库为准；空串表示本站未收录。 */
  benchmarkSlug: string;
  displayTier: DisplayTier;
  categories: BenchmarkCategory[];
  contamination: string;
  harness: string;
  metricCost: string;
  cadence: string;
  verifiedAt: string;
  notes: string[];
}

export interface BenchmarkSourcesContent {
  version: number;
  updatedAt: string;
  title: string;
  intro: string;
  tierLegend: TierLegendItem[];
  columns: SourceColumn[];
  sources: BenchmarkSource[];
}

export const benchmarkSources = content as BenchmarkSourcesContent;

/** 分组顺序：只列站内 `category` 枚举里有的值，其余落 other。 */
export const categoryOrder: readonly BenchmarkCategory[] = [
  'coding',
  'agent',
  'general',
  'math',
  'other',
];

export const categoryLabel: Record<string, string> = {
  coding: '编码与软件工程',
  agent: '工具与 agent',
  general: '通用与综合',
  math: '数学与推理',
  other: '其他',
};

export interface SourceGroup {
  category: BenchmarkCategory;
  label: string;
  items: BenchmarkSource[];
}

/** 按「第一个能识别的 category」分组，组内按名称升序。 */
export function groupSourcesByCategory(sources: readonly BenchmarkSource[]): SourceGroup[] {
  const primary = (source: BenchmarkSource): BenchmarkCategory =>
    source.categories.find((category) => categoryOrder.includes(category)) ?? 'other';

  return categoryOrder
    .map((category) => ({
      category,
      label: categoryLabel[category] ?? category,
      items: sources
        .filter((source) => primary(source) === category)
        .slice()
        .sort((a, b) => a.name.localeCompare(b.name, 'zh-CN')),
    }))
    .filter((group) => group.items.length > 0);
}
