import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { expect, test } from 'vitest';
import { DemoDownload, DemoPreview, tombstoneText } from './DemoDetailPage';

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

/**
 * 软删不再是墓碑。
 *
 * 旧断言写「已删除但 lifecycle 仍是 published 时走三种文案之一」，那是把
 * `deleted_at` 当成了墓碑的一种。生命周期 §4.1 相反：删了就是不存在（对访客 404），
 * 墓碑只属于 `archived` / `removed`。所以这里改为钉「非墓碑状态给兜底文案」，
 * 并确保兜底文案**不与 archived/removed 混同**——真走到它说明前后端假设不一致。
 */
test('非墓碑状态走兜底文案，且不与下架/移除混同', () => {
  const fallback = tombstoneText({ lifecycleState: 'published' });
  const archived = tombstoneText({ lifecycleState: 'archived' });
  const removed = tombstoneText({ lifecycleState: 'removed' });
  expect(fallback.title).not.toBe(archived.title);
  expect(fallback.title).not.toBe(removed.title);
});

/**
 * 预览的 sandbox 属性是第二道隔离（第一道是服务端 CSP）。
 * 这两条断言不是形式检查：`allow-same-origin` 一加，脚本就能读主站的
 * cookie 与 localStorage，然后带着访问者身份去调任何接口。
 */
test('预览 iframe 不带 allow-same-origin', () => {
  render(
    <MemoryRouter>
      <DemoPreview slug="demo-abc" />
    </MemoryRouter>,
  );
  const frame = screen.getByTitle('作品预览');
  const sandbox = frame.getAttribute('sandbox') ?? '';
  expect(sandbox).toContain('allow-scripts');
  expect(sandbox).not.toContain('allow-same-origin');
  expect(sandbox).not.toContain('allow-top-navigation');
});

test('预览 iframe 指向该作品的预览路径', () => {
  render(
    <MemoryRouter>
      <DemoPreview slug="demo-abc" />
    </MemoryRouter>,
  );
  expect(screen.getByTitle('作品预览')).toHaveAttribute('src', '/preview/demo-abc/');
});

/**
 * 下载入口的 href 必须是 /download/{slug}。
 *
 * 这条断言钉的是「与 `/preview` 分家」这个决定：把下载塞回预览路径，
 * 浏览器会按 Content-Type 渲染而不是落盘，作品里的脚本就跑到页面里来了。
 */
test('下载入口指向该作品的下载路径', () => {
  render(
    <MemoryRouter>
      <DemoDownload slug="demo-abc" />
    </MemoryRouter>,
  );
  expect(screen.getByRole('link', { name: '下载作品' })).toHaveAttribute(
    'href',
    '/download/demo-abc',
  );
});
