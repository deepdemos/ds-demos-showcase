import raw from './benchmark-leaderboards.json';

/** 与 `benchmarks.category` 的取值域保持一致；多模态/安全目前没有榜，保留占位。 */
export type BoardCategory = 'coding' | 'agent' | 'math' | 'multimodal' | 'safety' | 'general';

export interface BoardTopEntry {
  model: string;
  value: number;
}

export interface BoardEntry {
  id: string;
  name: string;
  description: string;
  category: BoardCategory;
  /** 该榜最新一条记录的发布时间（YYYY-MM-DD）；没有日期为 null。 */
  latest: string | null;
  /** 该榜出现过的模型组数量。 */
  models: number;
  /** 前几名；总览只取每个模型在该榜的最高分。 */
  top: BoardTopEntry[];
}

export interface BenchmarkLeaderboardsContent {
  title: string;
  intro: string;
  updatedAt: string;
  source: string;
  boards: BoardEntry[];
}

export const benchmarkLeaderboards = raw as BenchmarkLeaderboardsContent;

export const boardCategoryOrder: BoardCategory[] = [
  'coding',
  'agent',
  'math',
  'multimodal',
  'safety',
  'general',
];

export const boardCategoryLabel: Record<BoardCategory, string> = {
  coding: '编码与软件工程',
  agent: '工具与 agent',
  math: '数学与推理',
  multimodal: '多模态',
  safety: '安全',
  general: '通用、知识与推理',
};

export type Freshness = 'recent' | 'y2026' | 'all';

/** 以内容文件的 updatedAt 为基准，往前推 N 个月的日期（YYYY-MM-DD）。 */
export function freshCutoff(updatedAt: string, months = 6): string {
  const d = new Date(updatedAt + 'T00:00:00Z');
  d.setUTCMonth(d.getUTCMonth() - months);
  return d.toISOString().slice(0, 10);
}

/** 榜是否落在所选新鲜度范围内；没有日期的一律算旧榜。 */
export function boardMatchesFreshness(
  latest: string | null,
  fresh: Freshness,
  cutoff: string,
  yearStart: string,
): boolean {
  if (fresh === 'all') return true;
  if (!latest) return false;
  return fresh === 'recent' ? latest >= cutoff : latest >= yearStart;
}
