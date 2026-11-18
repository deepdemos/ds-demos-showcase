/**
 * 本机浏览历史（#426 的第一半，纯前端）。
 *
 * # 为什么存在 localStorage 而不是后端
 *
 * HIST-001：未登录访客也能看「最近看过什么」，不要求注册。这条要求直接排除了
 * 「先登录、后记录」的做法；而 HIST-011/012 又要求存储失败不能中断浏览、不能强制登录。
 * 所以这一半没有端点、没有表，只有这个浏览器里的一份快照。
 *
 * # 为什么存标题与封面的快照
 *
 * 历史页没有批量端点，逐件回源最多 100 次请求；而且作品被删之后回源只会 404，
 * 整页变成空白。快照让「我看过什么」在作品消失后仍然成立（HIST-004/005 的去重、
 * 上限都由本地决定）。
 *
 * # 失败静默
 *
 * 隐私模式、配额满、localStorage 被禁用时，存与读都会抛。这里每个入口都 catch：
 * 浏览本身比「记住这次浏览」重要。读失败给空数组，写失败什么都不做。
 */

export interface HistoryEntry {
  slug: string;
  title: string;
  coverUrl: string | null;
  /** ISO 时刻；同一作品只保留较近的一次。 */
  viewedAt: string;
}

/** 本机上限的默认值（HIST-006：这是前端偏好，后端不读它）。 */
export const HISTORY_LIMIT_DEFAULT = 100;
/** 界面允许的上限范围。上限是偏好，不是业务约束，所以不做成设置项。 */
export const HISTORY_LIMIT_MIN = 1;
export const HISTORY_LIMIT_MAX = 500;

export const HISTORY_STORAGE_KEY = 'demo-history-v1';
export const HISTORY_LIMIT_STORAGE_KEY = 'demo-history-limit-v1';

function storage(): Storage | null {
  try {
    return window.localStorage;
  } catch {
    // Safari 隐私模式 / 站点数据被禁用时，连读属性都会抛。
    return null;
  }
}

function isEntry(value: unknown): value is HistoryEntry {
  if (typeof value !== 'object' || value === null) return false;
  const item = value as Record<string, unknown>;
  return (
    typeof item.slug === 'string' &&
    item.slug.length > 0 &&
    typeof item.title === 'string' &&
    (item.coverUrl === null || typeof item.coverUrl === 'string') &&
    typeof item.viewedAt === 'string' &&
    !Number.isNaN(Date.parse(item.viewedAt))
  );
}

/** 读全部本机历史，按时间新→旧；损坏的条目被丢掉，不整页报错。 */
export function readHistory(): HistoryEntry[] {
  const store = storage();
  if (!store) return [];
  try {
    const raw = store.getItem(HISTORY_STORAGE_KEY);
    if (!raw) return [];
    const parsed: unknown = JSON.parse(raw);
    if (!Array.isArray(parsed)) return [];
    return parsed.filter(isEntry).sort((a, b) => b.viewedAt.localeCompare(a.viewedAt));
  } catch {
    return [];
  }
}

function writeEntries(entries: HistoryEntry[]): void {
  const store = storage();
  if (!store) return;
  try {
    store.setItem(HISTORY_STORAGE_KEY, JSON.stringify(entries));
  } catch {
    // 配额满 / 被禁用：这次没记住，但浏览继续。
  }
}

/** 记一次浏览：同一 slug 去重并提到最前，超过上限挤掉最旧的。 */
export function recordView(input: { slug: string; title: string; coverUrl: string | null }): void {
  if (!input.slug) return;
  const entry: HistoryEntry = {
    slug: input.slug,
    title: input.title,
    coverUrl: input.coverUrl ?? null,
    viewedAt: new Date().toISOString(),
  };
  const rest = readHistory().filter((item) => item.slug !== entry.slug);
  writeEntries([entry, ...rest].slice(0, readLimit()));
}

/** 删掉一条（按 slug）。 */
export function removeEntry(slug: string): void {
  writeEntries(readHistory().filter((item) => item.slug !== slug));
}

/** 清空本机历史。 */
export function clearHistory(): void {
  const store = storage();
  if (!store) return;
  try {
    store.removeItem(HISTORY_STORAGE_KEY);
  } catch {
    // 同上：清不掉也不该把页面打崩。
  }
}

/** 读本机上限（HIST-006）。损坏或越界时回默认值。 */
export function readLimit(): number {
  const store = storage();
  if (!store) return HISTORY_LIMIT_DEFAULT;
  try {
    const raw = store.getItem(HISTORY_LIMIT_STORAGE_KEY);
    const value = raw === null ? Number.NaN : Number.parseInt(raw, 10);
    if (!Number.isFinite(value)) return HISTORY_LIMIT_DEFAULT;
    return Math.min(HISTORY_LIMIT_MAX, Math.max(HISTORY_LIMIT_MIN, value));
  } catch {
    return HISTORY_LIMIT_DEFAULT;
  }
}

/** 写本机上限；下调时立刻裁剪已有记录，而不是等下一次浏览。 */
export function writeLimit(limit: number): void {
  const store = storage();
  const value = Math.min(
    HISTORY_LIMIT_MAX,
    Math.max(HISTORY_LIMIT_MIN, Math.trunc(Number.isFinite(limit) ? limit : HISTORY_LIMIT_DEFAULT)),
  );
  if (store) {
    try {
      store.setItem(HISTORY_LIMIT_STORAGE_KEY, String(value));
    } catch {
      return;
    }
  }
  const entries = readHistory();
  if (entries.length > value) writeEntries(entries.slice(0, value));
}
