import { mkdtempSync, readFileSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

import { test } from '@e2e-dev/web';
import { expect } from 'e2e';

/**
 * 生命周期、浏览计数、断点续传：三件「后端本来就有、但没人从外面走一遍」的事。
 *
 * # 为什么这三条非有不可
 *
 * 第四次独立 review 指出：e2e 只覆盖了投稿→审核→发布这一条主干，而
 * **生命周期的四个端点、浏览计数、Range** 一条用例都没有。它们又恰好是
 * 「单元测试全绿、真链路断掉」的高发区：
 *
 * - 下架 / 恢复要写审计与时间线（PR #269 刚补），写失败时接口仍然 200；
 * - 浏览计数是同一天同一人只算一次，靠的是唯一键而不是应用逻辑；
 * - 续传的最后一段才计数（PR #269 修的），半路的分段不该算。
 *
 * 这三条的共同点是：**判据在库与响应头里，不在页面的像素上**。所以除了点界面，
 * 还要用 `browser.evaluate` 在页面上下文里直接读 API 与响应头。
 *
 * # 前置条件
 *
 * 与闭环用例相同：服务已起好、库里灌了开发种子、有一个管理员账号
 * （CI 里由 `cmd/admin-grant` 现建），且 `E2E_API_LOG` 指向 API 日志
 * （注册要靠它拿验证码，见 closed-loop 的说明）。
 *
 * # 为什么每件作品都现建
 *
 * 种子数据只写库行，磁盘上没有文件（`db/seeds/0001-dev-demos.sql` 的注释写明了），
 * 所以下载 / Range 必须用真的上传过的作品。生命周期也必须作用在自己名下、
 * 已经发布的作品上——别人的作品按钮根本不会出现。
 */

const ADMIN_EMAIL = process.env.E2E_ADMIN_EMAIL ?? 'admin@e2e.local';
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? 'e2e-admin-password';

type Screen = import('e2e').Screen;
type App = import('e2e').App;
type Browser = import('@e2e-dev/web').Browser;

/** 从 API 日志里读验证码。判据与写法与 `closed-loop.e2e.ts` 一致：必须锚在「验证码是」之后。 */
function readVerificationCode(email: string): string {
  const path = process.env.E2E_API_LOG;
  if (!path) {
    throw new Error('E2E_API_LOG 未设置：注册要靠它从 API 日志里取验证码。');
  }
  const lines = readFileSync(path, 'utf8')
    .split('\n')
    .filter((line) => line.includes(`"${email}"`) && line.includes('验证码是'));
  if (lines.length === 0) {
    throw new Error(`日志里找不到发给 ${email} 的验证码（日志：${path}）`);
  }
  const match = /验证码是 (\d{6})/.exec(lines[lines.length - 1]);
  if (!match) throw new Error(`日志行里没有 6 位码：${lines[lines.length - 1]}`);
  return match[1];
}

async function loginAs(screen: Screen, app: App, email: string, password: string): Promise<void> {
  await app.open('/login');
  await screen.getByLabel('邮箱或用户名').fill(email);
  await screen.getByLabel('密码').fill(password);
  await screen.getByRole('button', { name: '登录' }).last().click();
  // 登录成功的判据是页头出现「登出」——停在哪一页都不能说明成功。
  await expect(screen.getByRole('button', { name: '登出' })).toBeVisible();
}

async function logout(screen: Screen): Promise<void> {
  await screen.getByRole('button', { name: '登出' }).click();
}

/**
 * 现建一件「已发布、作者自己登录着」的作品，返回 slug 与标题。
 *
 * 走的是完整链路：注册 → 验证邮箱 → 登录 → 投稿 → 管理员通过 → 发布 → 换回作者。
 * 每一步都必须是真实路径——用提权后门省掉哪一步，就等于那一部分从没被测过
 * （PR #220 的教训：测试绕过真实路径，415 一直绿着）。
 */
async function publishFreshDemo(
  app: App,
  screen: Screen,
  browser: Browser,
  tag: string,
): Promise<{ slug: string; title: string; email: string; password: string }> {
  const runId = `${Date.now().toString(36)}-${tag}`;
  const email = `e2e-${runId}@example.com`;
  const username = `e2e-${runId}`;
  const password = 'e2e-author-password';
  const title = `生命周期 ${runId}`;

  const dir = mkdtempSync(join(tmpdir(), 'e2e-lifecycle-'));
  const packagePath = join(dir, 'index.html');
  const body = `<!doctype html><meta charset="utf-8"><title>${title}</title><h1>e2e lifecycle</h1>`;
  writeFileSync(packagePath, body, 'utf8');

  await app.open('/register');
  await screen.getByLabel('邮箱').fill(email);
  await screen.getByLabel('用户名').fill(username);
  await screen.getByLabel(/密码/).fill(password);
  await screen.getByRole('button', { name: '注册' }).click();

  await expect(screen.getByRole('heading', { name: '验证邮箱' })).toBeVisible();
  await screen.getByLabel('验证码').fill(readVerificationCode(email));
  await screen.getByRole('button', { name: '完成验证' }).click();
  await expect(screen.getByRole('heading', { name: '登录' })).toBeVisible();

  await screen.getByLabel('邮箱或用户名').fill(email);
  await screen.getByLabel('密码').fill(password);
  await screen.getByRole('button', { name: '登录' }).last().click();
  await expect(screen.getByRole('button', { name: '登出' })).toBeVisible();

  await app.open('/submit');
  await screen.getByLabel('标题').fill(title);
  await screen.getByLabel(/简介/).fill('生命周期 e2e 用例创建，跑完可删。');
  await screen.getByLabel(/选择作品文件/).setInputFiles(packagePath);
  await screen.getByRole('button', { name: '提交投稿' }).click();
  await expect(screen.getByRole('heading', { name: '投稿已提交' })).toBeVisible();

  // slug 从成功页的预览链接里取——它是服务端给的那一份，不用自己再拼一个猜的。
  const previewLink = await screen.getByRole('link', { name: /预览/ }).getAttribute('href');
  const slug = (previewLink ?? '').replace(/^\/preview\//, '').replace(/\/$/, '');
  if (!slug || slug.startsWith('/')) {
    throw new Error(`投稿成功页的预览链接不对：${JSON.stringify(previewLink)}`);
  }

  // 换管理员：通过 + 发布。
  //
  // 这两步在用例里只是**前置条件**（被测的是生命周期、计数与 Range），所以走接口而不是
  // 点界面：审核页的「刚通过 · 待发布」卡片只活在本次会话里，队列里有残留条目时序号会
  // 错位，那个不稳定性会把真正的被测行为淹掉。管理员的界面路径由 closed-loop 用例覆盖。
  await logout(screen);
  await loginAs(screen, app, ADMIN_EMAIL, ADMIN_PASSWORD);

  const prep = await browser.evaluate(async (target: string) => {
    const headers = { 'Content-Type': 'application/json' };
    const approve = await fetch(`/api/v1/admin/reviews/demo/${target}`, {
      method: 'POST',
      headers,
      body: JSON.stringify({ decision: 'approve' }),
    });
    if (!approve.ok) {
      return { error: `通过失败 ${approve.status}：${await approve.text()}` };
    }
    const publish = await fetch(`/api/v1/admin/demos/${target}/publish`, {
      method: 'POST',
      headers,
      body: JSON.stringify({ channel: 'deep', visibility: 'listed' }),
    });
    if (!publish.ok) {
      return { error: `发布失败 ${publish.status}：${await publish.text()}` };
    }
    return { error: '' };
  }, slug);
  const prepError = (prep as { error: string } | null)?.error;
  if (prepError === undefined) throw new Error('browser.evaluate 没有回执');
  if (prepError !== '') throw new Error(prepError);

  // 换回作者：后面几个动作只有作者身份才看得到按钮。
  await logout(screen);
  await loginAs(screen, app, email, password);

  return { slug, title, email, password };
}

test('生命周期：下架、重新上架、删除、恢复，每一步的对外可见性都跟着变', async ({
  app,
  screen,
  browser,
}) => {
  const { slug, title } = await publishFreshDemo(app, screen, browser, 'life');

  // ---- 发布后作者能打开自己的作品 -------------------------------------
  await app.open(`/demos/${slug}`);
  await expect(screen.getByText(title)).toBeVisible();
  await expect(screen.getByRole('button', { name: '下架作品' })).toBeVisible();

  // ---- 下架：墓碑页，不再展示内容 -------------------------------------
  await screen.getByRole('button', { name: '下架作品' }).click();
  // 墓碑标题是 EmptyState 的正文（`<p>`），不是 heading——按 role 找会找不到。
  await expect(screen.getByText('作者已下架这个作品')).toBeVisible();

  // 墓碑是 200 而不是 404：它属于「曾经公开过」的档位，要能解释自己怎么没的。
  const archived = (await browser.evaluate(async (s: string) => {
    const response = await fetch(`/api/v1/demos/${s}`);
    const body = (await response.json()) as { lifecycleState?: string; title?: string | null };
    return {
      status: response.status,
      state: body.lifecycleState ?? null,
      title: body.title ?? null,
    };
  }, slug)) as { status: number; state: string | null; title: string | null } | null;
  if (!archived) throw new Error('页面上下文里取详情没有返回值');
  if (archived.status !== 200) {
    throw new Error(`下架后详情期望 200，实际 ${archived.status}`);
  }
  if (archived.state !== 'archived') {
    throw new Error(`下架后 lifecycleState 期望 archived，实际 ${JSON.stringify(archived.state)}`);
  }
  if (!archived.title) {
    throw new Error('墓碑页丢了标题：作者需要靠它认出这是哪件作品（实体域 §5.0）');
  }

  // ---- 重新上架：内容回来 ---------------------------------------------
  await screen.getByRole('button', { name: '重新上架' }).click();
  await expect(screen.getByText(title)).toBeVisible();

  // ---- 删除：两步确认，删完作者仍能打开（对外 404）---------------------
  await screen.getByRole('button', { name: '删除作品' }).click();
  // 第一次点只展开确认框，不能直接删。
  await expect(screen.getByRole('button', { name: '确认删除' })).toBeVisible();
  await screen.getByRole('button', { name: '确认删除' }).click();
  await expect(screen.getByText(/这件作品已删除，恢复期内可以恢复/)).toBeVisible();

  // 列表里已经看不到它（对外不可见的最直接证据）。
  await app.open('/demos');
  await expect(screen.getByText(title)).toHaveCount(0);

  // ---- 恢复：又回到公开状态 -------------------------------------------
  await app.open(`/demos/${slug}`);
  await expect(screen.getByRole('button', { name: '恢复作品' })).toBeVisible();
  await screen.getByRole('button', { name: '恢复作品' }).click();
  await expect(screen.getByText(title)).toBeVisible();
  await expect(screen.getByRole('button', { name: '下架作品' })).toBeVisible();
});

test('浏览计数：同一个人同一天反复打开，只算一次', async ({ app, screen }) => {
  // 种子里的作品（`db/seeds/0001-dev-demos.sql`）。这条只验「去重」，
  // 不需要自己上传，也不必是自己的作品——用固定 slug 比从列表里猜第一张卡片稳。
  const slug = 'piano-roll';

  // 第一次打开：这次只负责「有一次浏览」，页面上还看不到统计行——
  // `DemoDetailPage` 只在 `viewCount > 0` 时才渲染那一行，而计数是页面挂载之后
  // 才写进库的（POST /view），这一次读到的还是 0。
  await app.open(`/demos/${slug}`);
  await expect(screen.getByRole('heading', { name: /钢琴卷帘/ })).toBeVisible();

  const readCount = async (): Promise<number> => {
    await expect(screen.getByText(/浏览 \d+ 次/)).toBeVisible();
    const text = (await screen.getByText(/浏览 \d+ 次/).textContent()) ?? '';
    const value = Number(/浏览 ([\d,]+) 次/.exec(text)?.[1]?.replaceAll(',', '') ?? NaN);
    if (!Number.isInteger(value)) throw new Error(`读不出浏览量：${JSON.stringify(text)}`);
    return value;
  };

  // 第二次打开：统计行现在有了，记下它的数。
  await app.open(`/demos/${slug}`);
  const before = await readCount();
  if (before < 1) throw new Error(`浏览量该至少是 1，实际 ${before}`);

  // 第三次打开：同一天同一人只该算一次，数不能变。
  await app.open(`/demos/${slug}`);
  const after = await readCount();
  if (after !== before) {
    throw new Error(`同一天重复打开不该增加浏览量：再打开前 ${before}、之后 ${after}`);
  }
});

test('断点续传：分段请求给 206 与 Content-Range，越界给 416，尾段才计入下载数', async ({
  app,
  screen,
  browser,
}) => {
  const { slug } = await publishFreshDemo(app, screen, browser, 'range');

  /**
   * 读下载数。
   *
   * 什么时候写库是服务端的事：计数发生在响应体发完之后，用一个独立的 context
   * （`context.WithoutCancel`），所以它可能比 `fetch` 的 promise 晚一步落地。
   * 这里等一会儿再读，而不是立刻断言一次——立刻读会让这条用例时灵时不灵。
   */
  const downloadCount = async (): Promise<number> => {
    const read = async (): Promise<number> => {
      const value = await browser.evaluate(async (s: string) => {
        const response = await fetch(`/api/v1/demos/${s}`);
        const body = (await response.json()) as { downloadCount?: number };
        return body.downloadCount ?? 0;
      }, slug);
      return typeof value === 'number' ? value : 0;
    };
    for (let attempt = 0; attempt < 20; attempt += 1) {
      const value = await read();
      if (value > 0) return value;
      await new Promise((resolve) => setTimeout(resolve, 100));
    }
    return 0;
  };

  const startCount = await downloadCount();

  const probe = await browser.evaluate(async (s: string) => {
    const head = await fetch(`/download/${s}`, { method: 'HEAD' });
    const size = Number(head.headers.get('content-length') ?? '0');

    const partial = await fetch(`/download/${s}`, { headers: { Range: 'bytes=0-3' } });
    const partialBody = await partial.text();

    const outOfRange = await fetch(`/download/${s}`, { headers: { Range: 'bytes=999999-' } });

    return {
      size,
      acceptRanges: head.headers.get('accept-ranges'),
      headStatus: head.status,
      partialStatus: partial.status,
      partialRange: partial.headers.get('content-range'),
      partialLength: partialBody.length,
      outOfRangeStatus: outOfRange.status,
      outOfRangeHeader: outOfRange.headers.get('content-range'),
    };
  }, slug);

  if (probe.size <= 4) throw new Error(`下载体积异常：${probe.size}`);
  if (probe.acceptRanges !== 'bytes') {
    throw new Error(`缺 Accept-Ranges，实际 ${JSON.stringify(probe.acceptRanges)}`);
  }
  if (probe.partialStatus !== 206 || probe.partialLength !== 4) {
    throw new Error(
      `bytes=0-3 期望 206 且 4 字节，实际 ${probe.partialStatus} / ${probe.partialLength} 字节`,
    );
  }
  if (probe.partialRange !== `bytes 0-3/${probe.size}`) {
    throw new Error(`Content-Range 不对：${JSON.stringify(probe.partialRange)}`);
  }
  if (probe.outOfRangeStatus !== 416 || probe.outOfRangeHeader !== `bytes */${probe.size}`) {
    throw new Error(
      `越界期望 416 + bytes */${probe.size}，实际 ${probe.outOfRangeStatus} / ${probe.outOfRangeHeader}`,
    );
  }

  // 半路的分段与越界请求都不该计数（判据是「这一段是否到了文件末尾」）。
  const midCount = await downloadCount();
  if (midCount !== startCount) {
    throw new Error(`半路分段不该计入下载数：开跑前 ${startCount}、半路分段之后 ${midCount}`);
  }

  // 尾段：从中间到结尾，就是浏览器续传的最后一次请求。
  const tailStatus = await browser.evaluate(async (s: string) => {
    const head = await fetch(`/download/${s}`, { method: 'HEAD' });
    const size = Number(head.headers.get('content-length') ?? '0');
    const tail = await fetch(`/download/${s}`, {
      headers: { Range: `bytes=${Math.floor(size / 2)}-` },
    });
    await tail.arrayBuffer();
    return tail.status;
  }, slug);

  if (tailStatus !== 206) {
    throw new Error(`尾段请求期望 206，实际 ${tailStatus}`);
  }

  // 尾段到了末尾，算一次完整下载（PR #269 的修法）。
  const endCount = await downloadCount();
  if (endCount !== startCount + 1) {
    throw new Error(`尾段到达后下载数该 +1：开跑前 ${startCount}、现在 ${endCount}`);
  }
});
