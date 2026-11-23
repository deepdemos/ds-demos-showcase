import { test } from '@e2e-dev/web';
import { expect } from 'e2e';

/**
 * 基准链路：目录 → 总览 → 列头 → 详情（#574）。
 *
 * # 为什么这条链路要单独钉
 *
 * 它要三件事同时成立才走得通：总览入口的路由是通的、矩阵列头的链接指向自己那张榜、
 * 库里真的有能进矩阵的分数。任何一条断了，另外两条都还可能看着正常——空矩阵照样画表头，
 * 列头 href 写错照样点得动，路由接错时页面也可能只是一个壳。
 *
 * 所以失败信息分成四类，CI 报告里能直接看出该修哪一层：
 *
 * - `[路由没到]`：点「总览」之后没停在 /benchmarks/overview，或点列头之后没落在该榜的地址；
 * - `[数据为空]`：总览是空态、矩阵一行都没有、找不到种子里那张榜；
 * - `[链接不对]`：列头 href 不是 /benchmarks/<slug> 形状，或点进去的详情页不是列头那张榜；
 * - `[数据加载失败]`：页面是错误态（接口 503 / 库没连上）。单独标出来，免得把
 *   「服务没起」读成「数据是空的」。
 *
 * # 为什么按 role + 可见文本取元素
 *
 * 列会随数据变化：按下标取「第几列」，今天指向 HumanEval、换一版种子就指到别的榜上。
 * 所以先读渲染出来的列头文字，再用文字取链接；断言也拿列头文字去比 h1，
 * 而不是拿常量去比——常量只用来决定「点哪一列」，用错了应该表现为「找不到列」。
 *
 * # 依赖
 *
 * 与其它冒烟用例相同：服务已经起着（`e2e.config.ts` 只写 `app.url`），库里灌过
 * `db/seeds/0003-dev-benchmarks.sql`（CI 的 e2e job 有这一步；本地见
 * `docs/开发环境.md` 的「浏览器端到端冒烟（e2e）」一节）。用例只读，不写库。
 */

type Browser = import('@e2e-dev/web').Browser;

/** 取 pathname：`browser.url()` 给绝对地址，Link 渲染出来的 href 是根相对路径，两种都收。 */
function pathnameOf(value: string): string {
  const path = value.startsWith('/') ? value : new URL(value).pathname;
  const pathname = (path.split(/[?#]/)[0] ?? path).replace(/\/+$/, '');
  return pathname === '' ? '/' : pathname;
}

/** 等落点变成期望的 pathname；等不到就以「路由没到」收口。 */
async function waitForPathname(browser: Browser, expected: string, what: string): Promise<void> {
  try {
    await expect
      .poll(async () => pathnameOf(await browser.url()), { timeout: 15_000 })
      .toBe(expected);
  } catch {
    const actual = await browser.url();
    throw new Error(
      `[路由没到] ${what}：期望停在 ${expected}，实际是 ${pathnameOf(actual)}（${actual}）。` +
        '这是跳转目标或路由接错，不是数据问题。',
    );
  }
}

test('基准：目录 → 总览 → 列头 → 详情', async ({ app, screen, browser }) => {
  // 种子里两张 table 档榜之一（`db/seeds/0003-dev-benchmarks.sql`）。只用来决定点哪一列，
  // 断言拿读到的列头文字去比——种子改了名字，这里应该表现为「找不到列」而不是「h1 不对」。
  const seedBench = 'HumanEval';

  // ---- 1. 目录页 →「总览」--------------------------------------------------
  await app.open('/benchmarks');
  await expect(screen.getByRole('heading', { name: '第三方基准' })).toBeVisible({
    timeout: 15_000,
  });

  await screen.getByRole('link', { name: '总览' }).click();
  await waitForPathname(browser, '/benchmarks/overview', '从基准目录点「总览」');

  // ---- 2. 矩阵至少一行一列（不是空态）--------------------------------------
  //
  // 行：表格里每个模型是一个 `<th scope="row">`，它就是「至少一行」。
  // 表格 / 空态 / 错误态三者互斥，三样一起等：数据一到位就能分开走，
  // 不必先干等满 20 秒才知道「是空的」或「接口挂了」。
  const firstRow = screen.getByRole('rowheader').first();
  const emptyNotice = screen.getByText('还没有能放进矩阵的分数');
  const errorNotice = screen.getByText('基准总览加载失败');
  try {
    await expect(firstRow.or(emptyNotice).or(errorNotice)).toBeVisible({ timeout: 20_000 });
  } catch {
    throw new Error(
      '[数据为空] 等了 20 秒，矩阵一行都没有出现，也没有空态或错误提示：表格里没有任何数据行。',
    );
  }

  if ((await errorNotice.count()) > 0) {
    throw new Error(
      '[数据加载失败] 总览没有可陈列的数据：页面显示「基准总览加载失败」。' +
        '先看 GET /api/v1/benchmarks/overview 是否 200（服务没起、库没连上都会走到这里），' +
        '这既不是路由问题也不是链接问题。',
    );
  }

  if ((await emptyNotice.count()) > 0) {
    throw new Error(
      '[数据为空] 总览渲染的是空态「还没有能放进矩阵的分数」。' +
        '总览只收「table」档、有主指标、且分数已匹配到站内模型的榜；' +
        '灌过 db/seeds/0003-dev-benchmarks.sql 时这里该有 HumanEval / MMLU 两列。' +
        '空态说明种子没灌进去，或者收录口径把两张榜都过滤掉了。',
    );
  }

  // 列：列头里每一个「榜名链接」就是一列（第一列是「模型」，没有链接）。
  const columnLinks = screen.getByRole('columnheader').getByRole('link');
  const columnNames = await columnLinks.allTextContents();
  if (columnNames.length === 0) {
    throw new Error(
      '[数据为空] 矩阵有行没有列：列头里一个榜名链接都没有。' +
        '页面只在 columns 非空时才渲染表格，出现「有行无列」说明列头把榜名丢掉了。',
    );
  }

  const columnHrefs: string[] = [];
  for (const link of await columnLinks.all()) {
    columnHrefs.push((await link.getAttribute('href')) ?? '');
  }

  // 先把每一列的 href 都验一遍：形状不对的话，点进去必然是错的另一张页。
  const slugs = new Set<string>();
  for (const [index, name] of columnNames.entries()) {
    const href = columnHrefs[index] ?? '';
    const match = /^\/benchmarks\/([^/]+)$/.exec(pathnameOf(href));
    if (match === null) {
      throw new Error(
        `[链接不对] 列头「${name}」的 href 是 ${JSON.stringify(href)}，不是 /benchmarks/<slug> 的形状：` +
          '点下去落不到这张榜的详情页。',
      );
    }
    const slug = match[1] ?? '';
    if (slug === 'overview' || slug === 'sources') {
      throw new Error(
        `[链接不对] 列头「${name}」指向 /benchmarks/${slug}：那是站内的静态页面，不是任何一张榜的详情。`,
      );
    }
    if (slugs.has(slug)) {
      throw new Error(
        `[链接不对] 两张列头指向同一个地址 /benchmarks/${slug}，其中一张的 slug 是错的。`,
      );
    }
    slugs.add(slug);
  }

  // ---- 3. 点列头 → 该榜详情 -----------------------------------------------
  const targetIndex = columnNames.indexOf(seedBench);
  if (targetIndex === -1) {
    throw new Error(
      `[数据为空] 总览矩阵里找不到「${seedBench}」这一列（现有列：${columnNames.join('、')}）。` +
        'CI 的 e2e job 会灌 db/seeds/0003-dev-benchmarks.sql：这张 table 档榜有 pass@1 主指标，' +
        '分数也匹配到了站内模型，它应该占一列；没有就说明种子没灌，或收录口径把它过滤掉了。',
    );
  }
  const targetName = columnNames[targetIndex] ?? seedBench;
  const targetHref = columnHrefs[targetIndex] ?? '';

  // 按 role + 可见文本取，不按列下标：同一列在别的数据里可能排到别的位置。
  const targetLink = screen.getByRole('columnheader').getByRole('link', { name: targetName });
  await targetLink.click();
  await waitForPathname(browser, pathnameOf(targetHref), `点总览矩阵里「${targetName}」的列头`);

  // 详情页的 h1 是榜名（`Page title={detail.name}`），加载中会先是「基准」。
  const heading = screen.getByRole('heading', { level: 1 });
  let headingMatched = false;
  try {
    await expect
      .poll(async () => ((await heading.textContent()) ?? '').trim(), { timeout: 15_000 })
      .toBe(targetName);
    headingMatched = true;
  } catch {
    // 保持 false：下面给一句能指出「落到了别的页」的话，而不是把 poll 的断言错误原样抛出去。
  }
  if (!headingMatched) {
    const headingText = ((await heading.textContent()) ?? '').trim();
    throw new Error(
      `[链接不对] 点了列头「${targetName}」，详情页的 h1 却是「${headingText || '（空）'}」：` +
        `链接落到的不是这张榜的详情（落点 ${pathnameOf(await browser.url())}）。` +
        'h1 与列头文字必须一致，否则读者点开的和以为是的那张榜不是同一张。',
    );
  }
});
