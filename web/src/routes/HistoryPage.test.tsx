import { fireEvent, render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { beforeEach, describe, expect, test } from 'vitest';
import { HistoryPage } from './HistoryPage';
import {
  HISTORY_STORAGE_KEY,
  readHistory,
  readLimit,
  type HistoryEntry,
} from '@/features/history/local-history';

const entry = (slug: string, title: string, at: string): HistoryEntry => ({
  slug,
  title,
  coverUrl: null,
  viewedAt: at,
});

function seed(items: HistoryEntry[]) {
  localStorage.setItem(HISTORY_STORAGE_KEY, JSON.stringify(items));
}

function renderPage() {
  return render(
    <MemoryRouter>
      <HistoryPage />
    </MemoryRouter>,
  );
}

beforeEach(() => {
  localStorage.clear();
});

describe('HistoryPage', () => {
  test('列出本机记录，标题链到详情并标「本机」', () => {
    seed([
      entry('demo-a', '一号作品', '2026-11-02T00:00:00.000Z'),
      entry('demo-b', '二号作品', '2026-11-01T00:00:00.000Z'),
    ]);
    renderPage();

    expect(screen.getByText('共 2 条（本机）')).toBeInTheDocument();
    const link = screen.getByRole('link', { name: '一号作品' });
    expect(link).toHaveAttribute('href', '/demos/demo-a');
    expect(screen.getAllByText(/本机 · /)).toHaveLength(2);
    // 时间倒序：较近的一条排在前面。
    const titles = screen
      .getAllByRole('link')
      .filter((el) => el.getAttribute('href')?.startsWith('/demos/'));
    expect(titles.map((el) => el.textContent)).toEqual(['一号作品', '二号作品']);
  });

  test('删除一条只影响那一条', () => {
    seed([
      entry('demo-a', '一号作品', '2026-11-02T00:00:00.000Z'),
      entry('demo-b', '二号作品', '2026-11-01T00:00:00.000Z'),
    ]);
    renderPage();

    fireEvent.click(screen.getByRole('button', { name: '删除 一号作品' }));

    expect(screen.queryByRole('link', { name: '一号作品' })).toBeNull();
    expect(screen.getByRole('link', { name: '二号作品' })).toBeInTheDocument();
    expect(readHistory().map((item) => item.slug)).toEqual(['demo-b']);
  });

  test('清空之后回到空态', () => {
    seed([entry('demo-a', '一号作品', '2026-11-02T00:00:00.000Z')]);
    renderPage();

    fireEvent.click(screen.getByRole('button', { name: '清空' }));

    expect(screen.getByText('还没有浏览记录')).toBeInTheDocument();
    expect(readHistory()).toEqual([]);
  });

  test('把上限调小会立刻裁掉更旧的记录', () => {
    seed(
      Array.from({ length: 60 }, (_, i) =>
        entry('demo-' + i, '作品 ' + i, new Date(Date.UTC(2026, 10, 1, 0, i)).toISOString()),
      ),
    );
    renderPage();
    expect(screen.getByText('共 60 条（本机）')).toBeInTheDocument();

    fireEvent.change(screen.getByLabelText('本机历史上限'), { target: { value: '50' } });

    expect(readLimit()).toBe(50);
    expect(readHistory()).toHaveLength(50);
    expect(screen.getByText('共 50 条（本机）')).toBeInTheDocument();
  });

  test('没有记录时展示空态，不展示清空按钮', () => {
    renderPage();
    expect(screen.getByText('还没有浏览记录')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '清空' })).toBeDisabled();
  });
});
