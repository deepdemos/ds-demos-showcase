import { test } from '@e2e-dev/web';
import { expect } from 'e2e';

/**
 * 浏览器冒烟：只覆盖「页面能打开、跳到该去的地方、看得见主要内容」。
 *
 * # 为什么这一层非有不可
 *
 * tsc、eslint、vitest 全绿也挡不住下面这种事：
 * - dev 环境 vite 只代理了 `/api` 没代理 `/preview`，详情页的预览 iframe 永远空白；
 * - 投稿成功后跳到 `/demos/<slug>`，而草稿在详情页是 404；
 * - 未登录访问受保护页面没有回到登录页。
 *
 * 这三类都在「单元测试和类型检查都对、真浏览器里是坏的」之间。第 1 条是真实发生过的
 * （PR #218），第 2 类是真实存在过的 bug。它们只能靠真浏览器点出来。
 *
 * # 依赖
 *
 * 要求服务已经跑着（配置里只写 `app.url`，见 e2e.config.ts），且库里**灌了开发种子**
 * (`db/seeds/0001-dev-demos.sql`)——作品库那条用例需要有作品可点。
 * 这些用例只读，不写库、不投稿、不登录。
 */

test('首页打开并导航到作品库', async ({ app, screen }) => {
  await app.open('/');
  await expect(screen.getByRole('heading', { name: 'DeepDemos 展示' })).toBeVisible();

  await screen.getByRole('link', { name: '作品库' }).click();
  await expect(screen.getByRole('heading', { name: '作品库' })).toBeVisible();
});

test('作品库列出作品，点进去能看到详情', async ({ app, screen }) => {
  await app.open('/demos');
  await expect(screen.getByRole('heading', { name: '作品库' })).toBeVisible();

  // 种子里每条作品都没有封面，卡片会带「无封面」标记。用它可以区分
  // 「真的渲染出了卡片」和「只有一个空列表」。
  const card = screen.getByRole('link', { name: /无封面/ }).first();
  await expect(card).toBeVisible();
  await card.click();

  // 详情页的 h1 是作品标题，不论哪一条作品都会有。
  await expect(screen.getByRole('heading', { level: 1 })).toBeVisible();
  await expect(screen.getByRole('heading', { name: '预览' })).toBeVisible();
});

test('未登录访问待审队列会被送回登录页', async ({ app, screen }) => {
  await app.open('/admin/reviews');

  await expect(screen.getByRole('heading', { name: '登录' })).toBeVisible();
  await expect(screen.getByRole('link', { name: /注册/ })).toBeVisible();
});

test('未登录访问投稿页会被送回登录页', async ({ app, screen }) => {
  await app.open('/submit');

  await expect(screen.getByRole('heading', { name: '登录' })).toBeVisible();
});

/**
 * 这一条钉的是 PR #218 那类缺陷：vite 只代理了 `/api` 没代理 `/preview`，
 * 于是预览请求落回 SPA 的 index.html（200 + text/html）——页面看不出来，
 * iframe 里却是前端自己的壳。Go 的静态响应一定带 `X-Content-Type-Options: nosniff`，
 * 用它分辨请求到底被谁接住了。
 */
test('dev 代理把 /preview 交给后端，而不是落回前端 SPA', async ({ app, browser }) => {
  await app.open('/');

  const response = await browser.evaluate(async () => {
    const res = await fetch('/preview/e2e-no-such-slug/v1/index.html');
    return {
      status: res.status,
      contentType: res.headers.get('content-type') ?? '',
      nosniff: res.headers.get('x-content-type-options') ?? '',
    };
  });

  expect(response.nosniff).toBe('nosniff');
  expect(response.contentType).not.toContain('text/html');
  expect(response.status).toBe(404);
});

test('不存在的作品给出「作品不存在」而不是白屏', async ({ app, screen }) => {
  // 注意这里**不是**全局 404 页：详情页自己分辨「不存在 / 被移除 / 服务出错」，
  // 混成一句会误导作者（见 DemoDetailPage 的注释）。断言的就是这个具体文案。
  await app.open('/demos/e2e-no-such-slug');

  await expect(screen.getByText('作品不存在')).toBeVisible();
});
