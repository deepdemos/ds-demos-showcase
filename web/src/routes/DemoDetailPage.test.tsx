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

test('已删除但生命周期仍是 published：走第三种文案', () => {
  const text = tombstoneText({ lifecycleState: 'published' });
  expect(text.title).toContain('作者已删除');
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
