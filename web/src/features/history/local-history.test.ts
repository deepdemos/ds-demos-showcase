import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest';
import {
  clearHistory,
  HISTORY_LIMIT_DEFAULT,
  HISTORY_STORAGE_KEY,
  readHistory,
  readLimit,
  recordView,
  removeEntry,
  writeLimit,
} from './local-history';

const view = (slug: string, at = '2026-11-01T00:00:00.000Z') => ({
  slug,
  title: '作品 ' + slug,
  coverUrl: null,
  viewedAt: at,
});

beforeEach(() => {
  localStorage.clear();
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe('local-history', () => {
  test('记一次浏览后能读回来，带标题与封面快照', () => {
    recordView({ slug: 'demo-a', title: '一号作品', coverUrl: 'https://cdn/a.png' });
    const [entry] = readHistory();
    expect(entry.slug).toBe('demo-a');
    expect(entry.title).toBe('一号作品');
    expect(entry.coverUrl).toBe('https://cdn/a.png');
    expect(Number.isNaN(Date.parse(entry.viewedAt))).toBe(false);
  });

  test('同一作品看多次仍是一条，时刻取较近的一次且排在最前', () => {
    const first = '2026-11-01T00:00:00.000Z';
    localStorage.setItem(HISTORY_STORAGE_KEY, JSON.stringify([view('demo-a', first)]));
    recordView({ slug: 'demo-a', title: '一号作品', coverUrl: null });
    recordView({ slug: 'demo-b', title: '二号作品', coverUrl: null });

    const entries = readHistory();
    expect(entries.map((item) => item.slug)).toEqual(['demo-b', 'demo-a']);
    expect(entries[1].viewedAt > first).toBe(true);
  });

  test('上限调小后立刻裁剪，只留最近几条', () => {
    writeLimit(3);
    ['a', 'b', 'c', 'd'].forEach((slug) => recordView(view(slug)));
    const entries = readHistory();
    expect(entries).toHaveLength(3);
    expect(entries.map((item) => item.slug)).toEqual(['d', 'c', 'b']);
  });

  test('记录损坏时读回空数组，不把页面打崩', () => {
    localStorage.setItem(HISTORY_STORAGE_KEY, '{not json');
    expect(readHistory()).toEqual([]);
    localStorage.setItem(HISTORY_STORAGE_KEY, JSON.stringify([{ slug: 1 }, view('ok')]));
    expect(readHistory().map((item) => item.slug)).toEqual(['ok']);
  });

  test('删除与清空', () => {
    recordView(view('demo-a'));
    recordView(view('demo-b'));
    removeEntry('demo-a');
    expect(readHistory().map((item) => item.slug)).toEqual(['demo-b']);
    clearHistory();
    expect(readHistory()).toEqual([]);
  });

  test('上限读写的默认值与夹取', () => {
    expect(readLimit()).toBe(HISTORY_LIMIT_DEFAULT);
    writeLimit(20);
    expect(readLimit()).toBe(20);
    writeLimit(0);
    expect(readLimit()).toBe(1);
    writeLimit(9999);
    expect(readLimit()).toBe(500);
  });

  test('存储写入失败时浏览不受影响', () => {
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('QuotaExceededError');
    });
    expect(() => recordView(view('boom'))).not.toThrow();
    expect(readHistory()).toEqual([]);
  });
});
