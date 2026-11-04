import { mkdtempSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

import { test } from '@e2e-dev/web';
import { expect } from 'e2e';

/**
 * 七步闭环：投稿 → 待审 → 审核通过 → 发布 → 公开可见。
 *
 * # 为什么这条用例非有不可
 *
 * 它和 smoke.e2e.ts 覆盖的不是同一类问题。冒烟只证明「每个页面单独打开是对的」，
 * 而这条链路要三件事同时成立才走得通：
 *
 *   1. 上传真的把文件写进了存储（不是只在表单里选了个文件）；
 *   2. 审核通过**不等于**发布——发布是队列之外的一步，且只有管理员能做；
 *   3. 发布之后作品才对匿名访问者可见（列表与详情同时）。
 *
 * 这三条各自都有过「代码看着对、真跑起来不通」的先例：单文件扩展名从前端文件名取
 * （PR #220）、审核页原本根本没有发布按钮（PR #226）。任何一条断了，冒烟都是绿的。
 *
 * # 它写库
 *
 * 这条用例会真的建账号、真的上传、真的发布——没有删除端点，跑完那条作品会留在库里。
 * CI 用一次性容器，无所谓；本地请对着开发库跑，别对着有数据的库跑。
 *
 * # 前置条件
 *
 * - 服务已起好（vite + Go API + PostgreSQL），`APP_URL` 指向前端；
 * - 库里**有一个管理员**。CI 里由 `cmd/admin-grant` 现建；本地同样跑一次：
 *
 *     cd backend
 *     DATABASE_URL='...' ADMIN_GRANT_PASSWORD='e2e-admin-password' \
 *       go run ./cmd/admin-grant -email admin@e2e.local -username e2e-admin -display 测试管理员
 *
 *   账号名与口令可以用 `E2E_ADMIN_EMAIL` / `E2E_ADMIN_PASSWORD` 覆盖。
 *
 * # 为什么标题带 runId
 *
 * 本地反复跑时库里会累积多条同文案的作品，不带唯一后缀的话「页面上出现了这条标题」
 * 会被上一次跑剩下的那条满足——用例会绿，但它证明的是上一次的成功。
 */
const ADMIN_EMAIL = process.env.E2E_ADMIN_EMAIL ?? 'admin@e2e.local';
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? 'e2e-admin-password';

test('投稿 → 待审 → 通过 → 发布 → 公开可见', async ({ app, screen }) => {
  const runId = Date.now().toString(36);
  const email = `e2e-${runId}@example.com`;
  const username = `e2e-${runId}`;
  const authorPassword = 'e2e-author-password';
  const title = `闭环验证 ${runId}`;

  // 作品内容。不用种子里的作品：这条路要一个「刚刚上传的文件」，种子里的没有文件。
  const dir = mkdtempSync(join(tmpdir(), 'e2e-closed-loop-'));
  const packagePath = join(dir, 'index.html');
  writeFileSync(
    packagePath,
    '<!doctype html><meta charset="utf-8"><title>闭环验证</title><h1>e2e closed loop</h1>',
    'utf8',
  );

  // ---- 1. 作者注册（注册完即是登录态）------------------------------------
  await app.open('/register');
  await screen.getByLabel('邮箱').fill(email);
  await screen.getByLabel('用户名').fill(username);
  await screen.getByLabel(/密码/).fill(authorPassword);
  // 页头也有一个「登录」按钮（点了只是跳 /login），表单提交按钮在 DOM 后面。
  await screen.getByRole('button', { name: '注册' }).click();
  // 登录成功的判据是页头换成「登出」——停在哪一页都不能说明成功。
  await expect(screen.getByRole('button', { name: '登出' })).toBeVisible();

  // ---- 2. 投稿（真上传一个文件）------------------------------------------
  await app.open('/submit');
  await screen.getByLabel('标题').fill(title);
  await screen.getByLabel(/简介/).fill('闭环端到端用例创建，跑完可删。');
  await screen.getByLabel(/选择作品文件/).setInputFiles(packagePath);
  await screen.getByRole('button', { name: '提交投稿' }).click();
  await expect(screen.getByRole('heading', { name: '投稿已提交' })).toBeVisible();

  // ---- 3. 换管理员登录 ---------------------------------------------------
  await screen.getByRole('button', { name: '登出' }).click();
  await app.open('/login');
  await screen.getByLabel('邮箱或用户名').fill(ADMIN_EMAIL);
  await screen.getByLabel('密码').fill(ADMIN_PASSWORD);
  await screen.getByRole('button', { name: '登录' }).last().click();
  await expect(screen.getByRole('button', { name: '登出' })).toBeVisible();

  // ---- 4. 待审队列里能找到它 --------------------------------------------
  await app.open('/admin/reviews');
  await expect(screen.getByText(title)).toBeVisible();

  // ---- 5. 通过 -----------------------------------------------------------
  //
  // 队列里可能不止一条（本地反复跑会留下旧的待审/驳回记录），所以要按标题定位到
  // 那一条。Locator 没有 getByRole，只能先取全部列表项的文字算出序号，再点第 N 个
  // 「通过」——顺序与队列顺序一致（同一个 ul 渲染）。
  const items = await screen.getByRole('listitem').allTextContents();
  const index = items.findIndex((text) => text.includes(title));
  if (index < 0) {
    throw new Error(
      `待审队列里找不到「${title}」，队列共 ${items.length} 条：${items.join(' | ')}`,
    );
  }
  await screen.getByRole('button', { name: '通过' }).nth(index).click();

  // ---- 6. 发布（通过 ≠ 发布，这一步是单独的动作）-------------------------
  await expect(screen.getByText('刚通过 · 待发布')).toBeVisible();
  await screen.getByRole('button', { name: '发布' }).click();
  await expect(screen.getByText(/已发布到「主站」/)).toBeVisible();

  // ---- 7. 匿名访问者在作品库里看得到 ------------------------------------
  await screen.getByRole('button', { name: '登出' }).click();
  await app.open('/demos');
  await expect(screen.getByText(title)).toBeVisible();
});
