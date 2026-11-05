import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, expect, test, vi } from 'vitest';

const listDemoRevisions = vi.fn();
const restoreDemoRevision = vi.fn();

vi.mock('@/api', () => ({
  listDemoRevisions: (...args: unknown[]) => listDemoRevisions(...args),
  restoreDemoRevision: (...args: unknown[]) => restoreDemoRevision(...args),
}));

import { RevisionHistory } from './revision-history';

function revision(over: Partial<Record<string, unknown>> = {}) {
  return {
    revNo: 1,
    title: '第一版',
    demoType: 'web',
    source: 'upload',
    fileCount: 1,
    totalBytes: 100,
    createdAt: '2026-11-01T10:00:00Z',
    deleted: false,
    erased: false,
    // 默认造的是「旧版本」：不是当前版本，但是对外版本。
    // 设成 isCurrent: true 会让两行都显示「当前版本」，用例就测不到位置关系了。
    isCurrent: false,
    isPublished: true,
    ...over,
  };
}

beforeEach(() => {
  listDemoRevisions.mockReset();
  restoreDemoRevision.mockReset();
});

test('当前版本不给回滚按钮，旧版本给', async () => {
  listDemoRevisions.mockResolvedValue({
    status: 200,
    data: {
      items: [
        revision({ revNo: 2, title: '第二版', isCurrent: true, isPublished: false }),
        revision(),
      ],
    },
  });

  render(<RevisionHistory slug="demo-a" />);

  await waitFor(() => expect(screen.getByText('第 2 版')).toBeDefined());
  expect(screen.getByText('当前版本')).toBeDefined();
  expect(screen.getByText('对外版本')).toBeDefined();
  // 第 2 版是当前版本 → 只有一个回滚按钮，且它在第 1 版那一行。
  expect(screen.getAllByRole('button', { name: '回滚到这一版' })).toHaveLength(1);
});

test('擦除过的版本不给回滚按钮（回滚过去等于产出空版本）', async () => {
  listDemoRevisions.mockResolvedValue({
    status: 200,
    data: {
      items: [
        revision({ revNo: 2, title: '第二版', isCurrent: true, isPublished: false }),
        revision({ erased: true, deleted: true }),
      ],
    },
  });

  render(<RevisionHistory slug="demo-a" />);
  await waitFor(() => expect(screen.getByText('已擦除')).toBeDefined());
  expect(screen.queryByRole('button', { name: '回滚到这一版' })).toBeNull();
});

test('回滚失败时把后端的话原样显示出来', async () => {
  listDemoRevisions.mockResolvedValue({
    status: 200,
    data: {
      items: [revision({ revNo: 2, isCurrent: true, isPublished: false }), revision()],
    },
  });
  restoreDemoRevision.mockResolvedValue({
    status: 409,
    data: { code: 'conflict', message: '作品已被平台移除' },
  });

  render(<RevisionHistory slug="demo-a" />);
  await waitFor(() => expect(screen.getByText('第 2 版')).toBeDefined());

  fireEvent.click(screen.getByRole('button', { name: '回滚到这一版' }));

  await waitFor(() => expect(screen.getByText('作品已被平台移除')).toBeDefined());
  expect(restoreDemoRevision).toHaveBeenCalledWith('demo-a', 1);
});
