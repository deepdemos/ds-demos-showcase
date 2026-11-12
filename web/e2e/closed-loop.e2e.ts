import { mkdtempSync, readFileSync, writeFileSync } from 'node:fs';
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

/**
 * 从 API 日志里读某个邮箱收到的验证码。
 *
 * # 为什么读日志而不是读库
 *
 * 库里存的是 HMAC 哈希——拿不到明文，这是对的。测试要拿到码只有两条路：
 * 真收邮件（CI 里没有 SMTP），或者让开发期的邮件实现把正文写进日志
 * （`MAIL_LOG_CODES=1`，见 `backend/internal/platform/mailer/log.go`）。
 *
 * # 为什么这条用例不能跳过验证
 *
 * 「注册 → 验证 → 登录」是这一轮新加的一道门。用一个提权后门绕过去，等于把
 * 刚修好的东西从测试里抹掉——历史上正是这种「测试绕过真实路径」让
 * 单文件投稿的 415 一直绿着（见 PR #220 的教训）。
 */
function readVerificationCode(email: string): string {
  const path = process.env.E2E_API_LOG;
  if (!path) {
    throw new Error(
      'E2E_API_LOG 未设置：闭环用例需要从 API 日志里读验证码。' +
        '启动 API 时把输出重定向到文件，并把路径传进来（同时设 MAIL_LOG_CODES=1）。',
    );
  }
  const lines = readFileSync(path, 'utf8')
    .split('\n')
    // 日志是 JSON 行（slog），不是纯文本：邮件地址在 `"to":"..."` 里。
    // 同时按「正文里有验证码」再筛一次，避免把别的提到这个地址的行算进来。
    .filter((line) => line.includes(`"${email}"`) && line.includes('验证码是'));
  if (lines.length === 0) {
    throw new Error(`日志里找不到发给 ${email} 的验证码（日志：${path}）`);
  }
  // 取最后一封：重发过一次的话，早的那条可能已经过期。
  // 正则必须锚在「验证码是」后面：日志行是 JSON，前面 time 字段的微秒
  // （如 `0447852`）里也有连续 6 位数字，裸 `\d{6}` 会抓到它。
  const match = /验证码是 (\d{6})/.exec(lines[lines.length - 1]);
  if (!match) {
    throw new Error(`日志行里没有 6 位码：${lines[lines.length - 1]}`);
  }
  return match[1];
}

test('投稿 → 待审 → 通过 → 发布 → 公开可见 → 可下载', async ({ app, screen, browser }) => {
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

  // ---- 1. 作者注册 → 验证邮箱 → 登录 ------------------------------------
  //
  // 注册完**不是**登录态：账号落在 `pending`，要证明邮箱是自己的才能进站。
  await app.open('/register');
  await screen.getByLabel('邮箱').fill(email);
  await screen.getByLabel('用户名').fill(username);
  await screen.getByLabel(/密码/).fill(authorPassword);
  // 页头也有一个「登录」按钮（点了只是跳 /login），表单提交按钮在 DOM 后面。
  await screen.getByRole('button', { name: '注册' }).click();

  // 注册页把邮箱带到了验证页，这里不用再填一次。
  await expect(screen.getByRole('heading', { name: '验证邮箱' })).toBeVisible();

  // 这一条断言的是「没配 SMTP 时界面有没有说实话」。
  //
  // 开发机与 CI 都没配 SMTP，所以 `logMailer.Send` 返回 `ErrNotConfigured`，
  // 注册响应里 `verificationSent` 是 false，注册页带 `sent=0` 跳过来，这里就该出现提示条。
  // **如果哪天给 e2e 环境配了真 SMTP，这条断言要去掉**——那时信是真的发出去了。
  await expect(screen.getByText(/验证信没发出去/)).toBeVisible();

  // 但码仍然拿得到：logMailer 在返回错误**之前**已经把正文写进日志了
  // （`MAIL_LOG_CODES=1`）。「信没发出去」与「码在日志里」同时成立，不矛盾——
  // 前者是用户视角的事实，后者是开发环境的旁路。
  await screen.getByLabel('验证码').fill(readVerificationCode(email));
  await screen.getByRole('button', { name: '完成验证' }).click();

  // 验证成功跳登录页（不自动登录）。
  await expect(screen.getByRole('heading', { name: '登录' })).toBeVisible();
  await screen.getByLabel('邮箱或用户名').fill(email);
  await screen.getByLabel('密码').fill(authorPassword);
  await screen.getByRole('button', { name: '登录' }).last().click();
  // 登录成功的判据是页头换成「登出」——停在哪一页都不能说明成功。
  await expect(screen.getByRole('button', { name: '登出' })).toBeVisible();

  // ---- 2. 投稿（真上传一个文件）------------------------------------------
  await app.open('/submit');
  await screen.getByLabel('标题').fill(title);
  await screen.getByLabel(/简介/).fill('闭环端到端用例创建，跑完可删。');

  // 声明一个模型（DEMO-042 的「模型声明」那一项）。
  //
  // 模型目录是主数据，来自 `db/seeds/0002-dev-models.sql`——这条用例因此
  // **依赖那份种子已灌进库**（CI 里是显式的 psql 步骤）。灌不进去的话这里
  // 会超时，而不是静默通过：选了模型却没人记录，才是更坏的通过。
  await screen.getByLabel('DeepSeek Chat（DeepSeek）').click();

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

  // ---- 8. 匿名访问者能把作品下回去（DEMO-038~041）-----------------------
  //
  // 点进详情再从详情页下载：这条路径同时钉住三件事——
  //   a. 详情接口真的回了 downloadable=true（不是前端按类型猜的）；
  //   b. /download 端点对匿名访问者放行（可见性判定写对了）；
  //   c. 响应带 Content-Disposition: attachment（浏览器会落盘而不是渲染）。
  //
  // 用 fetch 直接读响应头而不是真点链接：`<a download>` 的落盘行为没法在
  // 页面上下文里断言，而「响应头对不对」才是我们真正能控制的那部分。
  await screen.getByText(title).click();

  // 模型声明跟着版本走：投稿时声明的那一个，发布后匿名访问者也看得到。
  // 这一条串起了「表单 → 表单字段 → 版本级声明 → 详情接口 → 页面」整条链，
  // 是全套里唯一一处真的从界面点出来、再从界面读回去的地方。
  //
  // 「未固定」也一起断言：投稿表单现在没有「用的是哪一版」这个字段，
  // 所以声明必然落在「作者没说」这一档。断言它出现，等于钉住
  // 「没填不等于最新版」——漏掉这段标记（或默认写成「最新版」）这条会红。
  //
  // 用 `\s*` 而不是字面拼接：两段文本之间有没有空白取决于排版（换个分隔符就会变），
  // 而这条要钉的是「标签之后跟的是未固定」，不是「中间没有空格」。
  await expect(screen.getByText(/模型声明：DeepSeek Chat\s*未固定/)).toBeVisible();

  const downloadLink = screen.getByRole('link', { name: '下载作品' });
  await expect(downloadLink).toBeVisible();

  // slug 从下载链接里取，而不是再拼一遍：链接是「服务端说能下」的那一份，
  // 用例跟着它走，等于顺带验了「前端展示的地址与后端认的 slug 是同一个」。
  const href = await downloadLink.getAttribute('href');
  const slug = (href ?? '').replace('/download/', '');
  if (!slug || slug === href) {
    throw new Error(`下载链接的 href 不对：${JSON.stringify(href)}`);
  }

  const download = await browser.evaluate(async (slug: string) => {
    const response = await fetch(`/download/${slug}`);
    return {
      status: response.status,
      disposition: response.headers.get('content-disposition') ?? '',
      contentType: response.headers.get('content-type') ?? '',
      length: (await response.blob()).size,
    };
  }, slug);

  if (download.status !== 200) {
    throw new Error(`下载期望 200，实际 ${download.status}（作品 ${slug}）`);
  }
  if (!download.disposition.includes('attachment')) {
    throw new Error(`下载响应缺 attachment，实际 ${JSON.stringify(download.disposition)}`);
  }
  if (download.length === 0) {
    throw new Error('下载回来的文件是空的');
  }
});
