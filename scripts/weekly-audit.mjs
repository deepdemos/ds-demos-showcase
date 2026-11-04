#!/usr/bin/env node
/**
 * 每周自查：把「靠心情才会做的检查」变成定时任务。
 *
 * # 为什么存在
 *
 * 2026-11-04 的结论是：四轮 review 抓了约 40 条，机器只拦了 4 条。差别不在我认真不认真，
 * 而在**有没有东西在我没主动想起它的时候跑一遍**。这个脚本就是那个东西：
 * 它只看三件能被机械判断的事，并且只报「确认为真」的问题。
 *
 * 1. **门禁是不是都还绿**（自己跑一遍，不信上一次 CI 的记忆）；
 * 2. **文档漂移**：`docs/已知松点.md` 里引用到的文件/路径还在不在——
 *    松点写「这里的判定在 `backend/x.go`」，而那个文件被移走了，这条松点就变成了假的；
 * 3. **债务存量**：单一出处基线里还欠着多少处副本，比上周多了还是少了。
 *
 * # 它不做什么
 *
 * 它不判断「这条松点还该不该留着」——那需要人。它只保证「这条松点引用的东西还真实存在」。
 * 一个不存在的文件比一条过期松点更糟：读者会去找它，然后怀疑整份文档。
 *
 * 用法：
 *   node scripts/weekly-audit.mjs            # 打到 stdout（Markdown）
 *   node scripts/weekly-audit.mjs --issue    # 有真问题才建 issue（复用同一个标题，避免每周刷屏）
 */
import { execFileSync } from 'node:child_process';
import { existsSync, readFileSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const ROOT = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const LOOSE_DOC = join(ROOT, 'docs', '已知松点.md');
const BASELINE = join(ROOT, 'scripts', 'single-source.baseline.json');

/** 每周自查跑的门禁：都是静态的、几秒内能跑完的。 */
const GATES = [
  'scripts/check-single-source.mjs',
  'scripts/check-negative-cases.mjs',
  'scripts/check-api-compat.mjs',
  'scripts/check-contracts.mjs',
  'scripts/check-constants.mjs',
  'scripts/check-objects.mjs',
  'scripts/check-migrations.mjs',
  'scripts/check-layering.mjs',
];

/** 这些扩展名出现在反引号里时，被当成「仓库内路径」来验证是否存在。 */
const PATH_EXT = /\.(?:go|ts|tsx|js|mjs|json|sql|ya?ml|md|css|html)$/;

function runGate(script) {
  try {
    execFileSync(process.execPath, [join(ROOT, script)], { cwd: ROOT, stdio: 'pipe' });
    return { script, ok: true, output: '' };
  } catch (error) {
    const stdout = error?.stdout ? String(error.stdout) : '';
    const stderr = error?.stderr ? String(error.stderr) : '';
    return { script, ok: false, output: (stdout + stderr).trim() };
  }
}

/** 抽松点文档里的 `## N. 标题` 小节及其正文。 */
function looseSections(docPath = LOOSE_DOC) {
  const text = readFileSync(docPath, 'utf8');
  const lines = text.split('\n');
  const sections = [];
  let current = null;
  for (const line of lines) {
    const m = /^##\s*(\d+)\.\s*(.+)$/.exec(line);
    if (m) {
      if (current) sections.push(current);
      current = { n: Number(m[1]), title: m[2].trim(), body: [] };
      continue;
    }
    if (current) {
      if (/^##\s/.test(line)) {
        sections.push(current);
        current = null;
        continue;
      }
      current.body.push(line);
    }
  }
  if (current) sections.push(current);
  return sections;
}

/** 松点正文里提到的仓库内路径，是否有文件已经不在。 */
export function danglingReferences(docPath = LOOSE_DOC) {
  const gone = [];
  for (const section of looseSections(docPath)) {
    // 删除线是「这里已经变过」的记号（~~旧路径~~ → 新路径）。它记录历史，不是失效引用。
    const body = section.body.join('\n').replace(/~~[^~\n]*~~/g, '');
    for (const raw of body.matchAll(/`([^`\n]+)`/g)) {
      const token = raw[1].trim().replace(/:[\d–\-]+$/, ''); // 去掉 :143 / :143-150
      if (!PATH_EXT.test(token)) continue;
      if (token.includes('*') || token.includes('…') || token.startsWith('-')) continue;
      if (/^[a-z]+:\/\//.test(token)) continue;
      if (!token.includes('/')) continue; // 单个文件名无法定位，跳过
      // 松点里的路径有的从仓库根写（`backend/x.go`），有的从子模块写（`internal/x.go`）——
      // 两种都算对，只有三种位置都不存在才是真漂移。
      const candidates = [token, join('backend', token), join('web', token)];
      if (!candidates.some((rel) => existsSync(join(ROOT, rel)))) {
        gone.push({ n: section.n, title: section.title, token });
      }
    }
  }
  return gone;
}

/** 单一出处基线里还欠多少处副本，按规则分组。 */
function debtByRule() {
  if (!existsSync(BASELINE)) return {};
  const baseline = JSON.parse(readFileSync(BASELINE, 'utf8'));
  const counts = {};
  for (const [rule, files] of Object.entries(baseline)) {
    if (!files || typeof files !== 'object') continue;
    const total = Object.values(files).reduce((sum, n) => sum + (Number(n) || 0), 0);
    if (total > 0) counts[rule] = total;
  }
  return counts;
}

export function audit({ runGateFn = runGate, docPath = LOOSE_DOC } = {}) {
  const gates = GATES.map((g) => runGateFn(g));
  const failed = gates.filter((g) => !g.ok);
  const dangling = danglingReferences(docPath);
  const debt = debtByRule();
  const sections = looseSections(docPath);

  return { gates, failed, dangling, debt, looseCount: sections.length };
}

function render(result) {
  const lines = [];
  lines.push('# 每周自查');
  lines.push('');
  lines.push('三件机械可判的事：门禁还绿不绿、松点引用的东西还在不在、单一出处的欠账有没有变多。');
  lines.push('');
  lines.push('## 门禁');
  lines.push('');
  lines.push('| 门禁 | 结果 |');
  lines.push('| --- | --- |');
  for (const gate of result.gates) {
    lines.push(`| \`${gate.script}\` | ${gate.ok ? '绿' : '**红**'} |`);
  }
  if (result.failed.length > 0) {
    lines.push('');
    for (const gate of result.failed) {
      lines.push(`<details><summary>${gate.script} 的输出</summary>`);
      lines.push('');
      lines.push('```');
      lines.push(gate.output.slice(0, 4000));
      lines.push('```');
      lines.push('');
      lines.push('</details>');
      lines.push('');
    }
  }
  lines.push('');
  lines.push('## 文档漂移（松点里引用的路径已经不在了）');
  lines.push('');
  if (result.dangling.length === 0) {
    lines.push('没有失效引用。');
  } else {
    lines.push('| 松点点 | 标题 | 失效引用 |');
    lines.push('| --- | --- | --- |');
    for (const d of result.dangling) {
      lines.push(`| ${d.n} | ${d.title} | \`${d.token}\` |`);
    }
  }
  lines.push('');
  lines.push('## 债务存量');
  lines.push('');
  lines.push(`松点共 ${result.looseCount} 条。单一出处基线里的副本（按规则）：`);
  lines.push('');
  if (Object.keys(result.debt).length === 0) {
    lines.push('基线为空或不存在。');
  } else {
    lines.push('| 规则 | 存量副本 |');
    lines.push('| --- | --- |');
    for (const [rule, n] of Object.entries(result.debt)) lines.push(`| ${rule} | ${n} |`);
  }
  lines.push('');
  lines.push('> 这道门禁只防「变多」，不要求一次清零。存量靠后续改动顺手收。');
  return lines.join('\n');
}

/** 有真问题才算问题：门禁红或存在失效引用。债务存量只记录，不建 issue。 */
function hasProblem(result) {
  return result.failed.length > 0 || result.dangling.length > 0;
}

async function createIssue(markdown, result) {
  const token = process.env.GH_TOKEN ?? process.env.GITHUB_TOKEN;
  const repo = process.env.GITHUB_REPOSITORY;
  if (!token || !repo) {
    console.error('缺少 GH_TOKEN / GITHUB_REPOSITORY，无法建 issue。');
    process.exit(1);
  }
  const headers = {
    Authorization: `Bearer ${token}`,
    Accept: 'application/vnd.github+json',
    'User-Agent': 'ds-demos-weekly-audit',
    'Content-Type': 'application/json',
  };
  const title = '每周自查：门禁与文档漂移';
  // 同一个标题只留一个 open issue：每周新开等于把噪音搬到 issue 列表。
  const search = await fetch(
    `https://api.github.com/search/issues?q=repo:${repo}+is:issue+is:open+in:title+%22%E6%AF%8F%E5%91%A8%E8%87%AA%E6%9F%A5%22`,
    { headers },
  );
  const found = search.ok ? await search.json() : { items: [] };
  const existing = (found.items ?? []).find((item) => item.title === title);
  if (existing) {
    const comment = await fetch(`https://api.github.com/repos/${repo}/issues/${existing.number}/comments`, {
      method: 'POST',
      headers,
      body: JSON.stringify({ body: markdown }),
    });
    if (!comment.ok) throw new Error(`评论失败：${comment.status}`);
    console.log(`已追加到已有 issue #${existing.number}`);
    return;
  }
  const response = await fetch(`https://api.github.com/repos/${repo}/issues`, {
    method: 'POST',
    headers,
    body: JSON.stringify({
      title,
      body: markdown,
      // 只用仓库里已存在的标签：不存在的 label 会让建 issue 直接失败（422）。
      labels: ['ci', 'tech-debt'],
    }),
  });
  if (!response.ok) throw new Error(`建 issue 失败：${response.status} ${await response.text()}`);
  const issue = await response.json();
  console.log(`已建 issue #${issue.number}（门禁红 ${result.failed.length} 个，失效引用 ${result.dangling.length} 处）`);
}

const isMain = process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url);
if (isMain) {
  const result = audit();
  const markdown = render(result);
  console.log(markdown);
  if (process.argv.includes('--issue')) {
    if (!hasProblem(result)) {
      console.log('\n没有需要建 issue 的问题（门禁全绿、无失效引用）。');
    } else {
      await createIssue(markdown, result);
    }
  }
  if (hasProblem(result)) process.exitCode = 0; // 自查本身不是门禁，别把定时任务标红
}
