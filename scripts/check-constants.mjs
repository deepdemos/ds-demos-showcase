// 可变常量门禁：docs/运行手册-可变常量.md 里登记的每个「唯一改动点」必须还活着。
// 纯静态（零依赖），CI 与本地都能跑：node scripts/check-constants.mjs
//
// 为什么需要它：手册的价值全在「锚点还指着实物」。重构一次改名、把 §11.4 改成 §11.5、
// 或者把某条约束删了，手册就会变成一份过期的谎言——比没有手册更糟，因为它看起来可信。
// 这个脚本让「手册过期」变成 CI 红灯，而不是下一个人踩坑时才发现。
import fs from 'node:fs';
import path from 'node:path';
import { pathToFileURL } from 'node:url';

export const RUNBOOK = 'docs/运行手册-可变常量.md';

// 「唯一改动点」一格的形状：`路径` · `锚点`
// 两个反引号片段都是可选的第二个；第一个必须是仓库相对路径。
const CELL_SPLIT = '|';
const TICK = /`([^`]+)`/g;

function cellsOf(line) {
  // 表格行以 | 开头结尾，split 后首尾是空串
  const parts = line.split(CELL_SPLIT);
  if (parts.length < 6) return null;
  return parts.map((s) => s.trim());
}

/** 解析手册正文，返回 { declared, rows: [{ no, name, value, where, effect }] } */
export function parseRunbook(text) {
  const declaredMatch = text.match(/共登记\s*\*\*(\d+)\s*条\*\*/);
  const declared = declaredMatch ? Number(declaredMatch[1]) : null;

  const rows = [];
  for (const line of text.split('\n')) {
    if (!line.startsWith('|')) continue;
    const cells = cellsOf(line);
    if (!cells) continue;
    // cells: ['', '#', 常量, 值, 唯一改动点, 改了会怎样, '']
    if (!/^\d+$/.test(cells[1])) continue;
    rows.push({
      no: Number(cells[1]),
      name: cells[2],
      value: cells[3],
      where: cells[4],
      effect: cells[5],
      line,
    });
  }
  return { declared, rows };
}

/** 从「唯一改动点」一格抽出 [路径, 锚点?] */
export function parseAnchor(where) {
  const spans = [...where.matchAll(TICK)].map((m) => m[1]);
  return { spans, file: spans[0] || null, token: spans[1] || null };
}

/**
 * 校验手册。返回问题列表（空数组 = 通过）。
 * root 是仓库根（含 docs/ 与 db/ 的那一层）。
 */
export function inspectConstants(root = process.cwd()) {
  const problems = [];
  const abs = path.join(root, RUNBOOK);
  if (!fs.existsSync(abs)) return [`${RUNBOOK} 不存在`];

  const text = fs.readFileSync(abs, 'utf8');
  const { declared, rows } = parseRunbook(text);

  if (declared === null) problems.push(`${RUNBOOK} 文首没有「共登记 **N 条**」的条数声明（门禁无法核对完整性）`);
  if (rows.length === 0) problems.push(`${RUNBOOK} 没有解析到任何编号行（表格格式变了？）`);

  // 编号必须从 1 连续到 N
  for (let i = 0; i < rows.length; i++) {
    if (rows[i].no !== i + 1) {
      problems.push(`${RUNBOOK} 编号不连续：第 ${i + 1} 行是 #${rows[i].no}（应为 #${i + 1}）`);
      break;
    }
  }
  if (declared !== null && rows.length !== declared) {
    problems.push(`${RUNBOOK} 声明 ${declared} 条，实际解析到 ${rows.length} 条`);
  }

  // 注意：多条常量共用一个章节锚点是正常的（§11.4 就同时管密码长度、哈希参数、并发路数）。
  // 所以这里查的是「常量名重复」而不是「锚点重复」——后者会全是误报。
  const seenNames = new Map();
  for (const row of rows) {
    const tag = `#${row.no}「${row.name}」`;
    if (seenNames.has(row.name)) {
      problems.push(`${tag} 与 #${seenNames.get(row.name)} 的常量名重复（复制粘贴留下的重复行？）`);
    } else {
      seenNames.set(row.name, row.no);
    }
    const { file, token } = parseAnchor(row.where);
    if (!file) {
      problems.push(`${tag} 的「唯一改动点」没有登记任何文件路径（写成 \`路径\` · \`锚点\`）`);
      continue;
    }
    const target = path.join(root, file);
    if (!fs.existsSync(target)) {
      problems.push(`${tag} 的改动点 ${file} 不存在（文件被删或改名了）`);
      continue;
    }
    if (token) {
      const body = fs.readFileSync(target, 'utf8');
      if (!body.includes(token)) {
        problems.push(`${tag} 的锚点 \`${token}\` 在 ${file} 里找不到了（约束被删 / 章节改号 / 字段改名）`);
      }
    }
  }

  return problems;
}

export function stats(root = process.cwd()) {
  const abs = path.join(root, RUNBOOK);
  if (!fs.existsSync(abs)) return { declared: 0, rows: 0 };
  const { declared, rows } = parseRunbook(fs.readFileSync(abs, 'utf8'));
  return { declared: declared || 0, rows: rows.length };
}

export function run(root = process.cwd()) {
  const problems = inspectConstants(root);
  const st = stats(root);
  if (problems.length) {
    console.error(`可变常量门禁失败（${problems.length} 条）：`);
    for (const p of problems) console.error('  - ' + p);
    return 1;
  }
  console.log(`可变常量门禁通过（${RUNBOOK} 登记 ${st.rows} 条，锚点全部命中）`);
  return 0;
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) process.exit(run());
