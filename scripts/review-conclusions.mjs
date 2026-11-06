/**
 * review 结论落地：每条发现都必须变成三种东西之一，不许停在「已修」。
 *
 * # 为什么
 *
 * 2026-11-04 四轮独立 review 一共提出约 50 条发现。它们全都被「修了」——
 * 但没有任何一条变成门禁或测试。于是下一轮 review 又从零开始，而我在每轮之间
 * 重新犯同一类错（可见性判定写第二遍、认证绕过、正向用例盖住反向缺失）。
 *
 * 「已修」不可验证：改完就没了，没人能证明它不会再犯。
 * 真正能减熵的只有三种收口方式：
 *
 * 1. **gate / test** —— 变成机器每次都会跑的东西（最好的一种）；
 * 2. **loose** —— 认了不做，写进 `docs/已知松点.md`，带编号可追溯；
 * 3. **wontfix** —— 决定不改，但必须写理由（≥10 字），否则下一轮会被重新提出。
 *
 * # 格式（写在 review 正文里）
 *
 * ```
 * ## 结论落地
 * 共 8 条。
 * - F1 详情没判渠道 → test: TestGetDemoHiddenChannelIs404
 * - F2 可见性判定写了十二遍 → gate: scripts/check-single-source.mjs
 * - F3 历史版本下载 → loose: 30
 * - F4 单文件扩展名可伪造 → wontfix: 走魔数嗅探，伪造扩展名改不了解析结果
 * ```
 *
 * 「共 N 条」这一行是给人看的自检，也是本门禁唯一能验的完整性约束：
 * 声明几条就必须落地几条。没有它就只能验「至少一条」，那等于没约束。
 */
import { existsSync, readFileSync, readdirSync, statSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const ROOT = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const LOOSE_DOC = 'docs/已知松点.md';

export const HEADING = /^##\s*结论落地\s*$/m;
const COUNT_LINE = /共\s*(\d+)\s*条/;
const ITEM = /^\s*[-*]\s*(?:F|P|R)?\s*(\d+)[.、:：]?\s*(.+?)\s*(?:→|->)\s*(test|gate|loose|wontfix)\s*[:：]\s*(.+?)\s*$/;

/**
 * 这个文件名算不算「装着测试的文件」。
 *
 * 认四种：Go 的 `_test.go`、前端的 `.test.` / `.spec.`、`web/e2e/*.e2e.ts`、
 * 以及 `scripts/` 下门禁自己的 `.test.mjs`。三类都是「有东西钉着」的证据，
 * 早先依次漏过 e2e 与门禁自测，门禁于是把这类证据判成「测试不存在」——
 * 「这条判据有门禁用例钉着」反而过不了门禁，是它自己的形状错误。
 */
export function isTestFile(name) {
  return (
    /\.(test|spec)\.(_test\.)?(go|ts|tsx|mjs|js)$/.test(name) ||
    name.endsWith('_test.go') ||
    name.endsWith('.e2e.ts')
  );
}

function escapeRe(s) {
  return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}

/**
 * 在整个后端与前端测试里找这个测试名（Go 函数、前端用例名、e2e 用例标题）。
 *
 * e2e 也是测试：`web/e2e/*.e2e.ts` 里的 `test('…')` 标题同样可以作为结论的落点；
 * `scripts/` 下门禁自己的 `.test.mjs` 也是——「这条判据有门禁用例钉着」是最该被认的证据。
 * 早先的文件过滤只认 `.test.` / `.spec.` / `_test.go`，于是「这条能力有 e2e 钉着」
 * 反而过不了门禁——门禁本身把一类证据排除在外了。
 */
function findTest(name) {
  const roots = [join(ROOT, 'backend'), join(ROOT, 'web', 'src'), join(ROOT, 'web', 'e2e'), join(ROOT, 'scripts')];
  const goPattern = new RegExp(`^func\\s+${escapeRe(name)}\\s*\\(`, 'm');
  const tsPattern = new RegExp(`(?:^|\\s)(?:test|it)\\s*\\(\\s*['"\`]${escapeRe(name)}['"\`]`, 'm');
  const stack = [...roots];
  while (stack.length > 0) {
    const dir = stack.pop();
    if (!existsSync(dir)) continue;
    for (const entry of readdirSync(dir)) {
      if (entry === 'node_modules' || entry === 'dist' || entry === '.e2e') continue;
      const abs = join(dir, entry);
      const st = statSync(abs);
      if (st.isDirectory()) {
        stack.push(abs);
        continue;
      }
      if (!/\.(go|ts|tsx|mjs|js)$/.test(entry)) continue;
      if (!isTestFile(entry)) continue;
      const source = readFileSync(abs, 'utf8');
      if (goPattern.test(source) || tsPattern.test(source)) return true;
    }
  }
  return false;
}

/** 松点文档里是否真有第 N 条（`## N. 标题`）。 */
function hasLoosePoint(n) {
  const abs = join(ROOT, LOOSE_DOC);
  if (!existsSync(abs)) return false;
  return new RegExp(`^##\\s*${n}\\.\\s`, 'm').test(readFileSync(abs, 'utf8'));
}

/**
 * 校验一段 review 正文里的结论落地段落。
 *
 * @returns {{ok: boolean, problems: string[], landed: number, declared: number|null}}
 */
export function checkConclusions(body, { findTestFn = findTest, hasLoosePointFn = hasLoosePoint } = {}) {
  const problems = [];
  const text = typeof body === 'string' ? body : '';
  const match = HEADING.exec(text);
  if (!match) {
    return {
      ok: false,
      problems: ['review 正文缺少「## 结论落地」段落：每条发现必须变成 test / gate / 松点 / 明确不修之一，「已修」不算收口。'],
      landed: 0,
      declared: null,
    };
  }

  const section = text.slice(match.index + match[0].length);
  const declaredMatch = COUNT_LINE.exec(section);
  const declared = declaredMatch ? Number(declaredMatch[1]) : null;

  const items = [];
  for (const line of section.split('\n')) {
    if (/^##\s/.test(line)) break; // 下一个标题，段落结束
    const m = ITEM.exec(line);
    if (m) items.push({ id: m[1], what: m[2], kind: m[3], value: m[4] });
  }

  if (items.length === 0) {
    problems.push('「结论落地」里一条都没有：至少写一条 `- F1 摘要 → test: TestXxx`。');
  }
  if (declared !== null && declared !== items.length) {
    problems.push(`声明共 ${declared} 条，实际落地 ${items.length} 条——数目对不上，说明有发现被丢了。`);
  }
  if (declared === null && items.length > 0) {
    problems.push('缺少「共 N 条」这一行：没有它就无法判断有没有发现被丢掉。');
  }

  for (const item of items) {
    if (item.what.trim().length < 4) {
      problems.push(`第 ${item.id} 条的摘要太短（至少 4 个字，好让下一个人看懂说的是什么）。`);
    }
    if (item.kind === 'test') {
      if (!findTestFn(item.value)) {
        problems.push(`第 ${item.id} 条指向的测试「${item.value}」不存在（后端函数名 / 前端用例名都没找到）。`);
      }
    } else if (item.kind === 'gate') {
      const rel = item.value.replace(/^\.?\//, '');
      if (!existsSync(join(ROOT, rel))) {
        problems.push(`第 ${item.id} 条指向的门禁脚本「${item.value}」不存在。`);
      }
    } else if (item.kind === 'loose') {
      const n = Number(item.value.replace(/[^\d]/g, ''));
      if (!Number.isInteger(n) || !hasLoosePointFn(n)) {
        problems.push(`第 ${item.id} 条说记进松点 ${item.value}，但 ${LOOSE_DOC} 里没有这一条。`);
      }
    } else if (item.kind === 'wontfix') {
      if (item.value.trim().length < 10) {
        problems.push(`第 ${item.id} 条决定不修，但理由太短（至少 10 个字）——否则下一轮会被重新提出。`);
      }
    }
  }

  return { ok: problems.length === 0, problems, landed: items.length, declared };
}
