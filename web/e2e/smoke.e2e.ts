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
 * iframe 里却是前端自己的壳。
 *
 * 判据用两个只有 Go 的预览会带的东西：`X-Content-Type-Options: nosniff` 与
 * `Content-Security-Policy: sandbox …`。**不能再用「不是 text/html」**：
 * 预览的失败分支从 2026-11-09 起返回的是自己的中文 HTML 页（PR #359），
 * 那是后端答的，不是 SPA 兜的。
 */
test('dev 代理把 /preview 交给后端，而不是落回前端 SPA', async ({ app, browser }) => {
  await app.open('/');

  const response = await browser.evaluate(async () => {
    const res = await fetch('/preview/e2e-no-such-slug/v1/index.html');
    return {
      status: res.status,
      contentType: res.headers.get('content-type') ?? '',
      nosniff: res.headers.get('x-content-type-options') ?? '',
      csp: res.headers.get('content-security-policy') ?? '',
      body: (await res.text()).slice(0, 2000),
    };
  });

  expect(response.nosniff).toBe('nosniff');
  // 只有后端会带 sandbox 这条 CSP；SPA 的 index.html 不带。
  expect(response.csp).toContain('sandbox');
  // 后端答的是自己那张失败页，不是前端壳（SPA 的 index.html 里有 id="root"）。
  expect(response.body).toContain('这个作品现在打不开');
  expect(response.body).not.toContain('id="root"');
  expect(response.status).toBe(404);
});

test('不存在的作品给出「作品不存在」而不是白屏', async ({ app, screen }) => {
  // 注意这里**不是**全局 404 页：详情页自己分辨「不存在 / 被移除 / 服务出错」，
  // 混成一句会误导作者（见 DemoDetailPage 的注释）。断言的就是这个具体文案。
  await app.open('/demos/e2e-no-such-slug');

  await expect(screen.getByText('作品不存在')).toBeVisible();
});

/**
 * 模型页能打开，三块都在。
 *
 * 只断言「页面渲染出了三块」而不断言作品数：这条用例只依赖开发种子
 * （`db/seeds/0002-dev-models.sql` 里的 6 个模型），种子作品**没有**模型声明，
 * 所以「作品数」在干净的 CI 库里是 0——断言具体数字会与本地库耦合。
 *
 * 「详情页模型名是链接」这条由 `DemoDetailPage.test.tsx` 的用例钉住：它需要一件
 * 声明了模型的作品，而种子没有，真浏览器里造一件就得投稿（那属于闭环用例的范围）。
 */
test('模型页三块都渲染出来', async ({ app, screen }) => {
  await app.open('/models/deepseek-chat');

  await expect(screen.getByRole('heading', { name: 'DeepSeek Chat' })).toBeVisible();
  await expect(screen.getByRole('heading', { name: '档案' })).toBeVisible();
  await expect(screen.getByRole('heading', { name: '站内表现' })).toBeVisible();
  await expect(screen.getByRole('heading', { name: '第三方基准' })).toBeVisible();
});

test('不存在的模型给出「没有这个模型」而不是白屏', async ({ app, screen }) => {
  await app.open('/models/e2e-no-such-model');

  await expect(screen.getByText('没有这个模型')).toBeVisible();
});
