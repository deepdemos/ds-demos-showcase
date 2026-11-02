import { expect, test } from 'vitest';
import { tombstoneText } from './DemoDetailPage';

/**
 * 墓碑文案按「怎么消失的」分三种。标准对象 §ST-2 要求 archived 与 removed 的墓碑内容
 * **必须不同**——一个是作者自己下架，一个是平台移除，混成一句「作品不存在」会让作者
 * 以为自己的东西被删了。
 */
test('归档：文案说的是作者自己下架', () => {
  const text = tombstoneText({ lifecycleState: 'archived' });
  expect(text.title).toContain('作者已下架');
});

test('移除：文案说的是平台移除，不能与归档同句', () => {
  const archived = tombstoneText({ lifecycleState: 'archived' });
  const removed = tombstoneText({ lifecycleState: 'removed' });
  expect(removed.title).toContain('已被移除');
  expect(removed.title).not.toBe(archived.title);
  expect(removed.description).not.toBe(archived.description);
});

test('已删除但生命周期仍是 published：走第三种文案', () => {
  const text = tombstoneText({ lifecycleState: 'published' });
  expect(text.title).toContain('作者已删除');
});
