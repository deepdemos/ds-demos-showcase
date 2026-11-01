#!/usr/bin/env node
// 标准对象门禁：跨域共享的取值域只允许在 docs/标准对象.md 里定义一次。
//
// 三条断言：
//   A 重复枚举 —— 其他文档里出现与标准对象**集合完全相同**的取值枚举时，该行或上方 3 行内必须有
//      `标准对象 §<键>` 锚点；否则报错（说明这里又抄了一份事实源）。
//   B 值集走样 —— 列名相同且与标准对象**至少共享 2 个取值**、但集合不同，视为同一枚举写歪了
//      （历史事故：账号状态在另一份文档里写成 3 值，缺 pending/banned）。
//   C 引用键 —— 任何 `标准对象 §X` 的键必须是 `ST|EN|ID|TS|P` + 数字，且在登记表里存在。
//
// 放行规则：只被一个域消费的私有枚举（与标准对象交集 < 2）不参与比对，避免把无关枚举当冲突。
import fs from 'node:fs';
import path from 'node:path';

export const AUTHORITY = 'docs/标准对象.md';
const ROOT = process.cwd();
const KEY_PATTERN = /^§(ST|EN|ID|TS|P)-(\d+)$/;
const ANCHOR_PATTERN = /标准对象\s*§?(ST|EN|ID|TS|P)-(\d+)/;

/** 从标准对象文档里解析登记表与各对象的权威取值。 */
export function parseAuthority(text) {
  const keys = [];
  for (const line of text.split('\n')) {
    const match = line.match(/^\|\s*`((?:ST|EN|ID|TS|P)-\d+)`\s*\|/);
    if (match) keys.push(match[1]);
  }
  const objects = [];
  const sectionPattern = /^##\s*§((?:ST|EN|ID|TS|P)-\d+).*$/gm;
  const starts = [];
  for (const match of text.matchAll(sectionPattern)) {
    starts.push({ key: match[1], index: match.index });
  }
  starts.forEach((section, i) => {
    const body = text.slice(section.index, i + 1 < starts.length ? starts[i + 1].index : text.length);
    for (const found of extractChecks(body)) {
      objects.push({ key: section.key, ...found });
      break; // 每个对象的第一处 CHECK 视为权威取值域
    }
  });
  return { keys, objects };
}

/** 抽出 `CHECK (col IN ('a','b'))` 形式的取值枚举。 */
export function extractChecks(text) {
  const result = [];
  const pattern = /CHECK\s*\(\s*(\w+)\s+IN\s*\(([^)]*)\)/g;
  for (const match of text.matchAll(pattern)) {
    const values = [...match[2].matchAll(/'([^']*)'/g)].map((v) => v[1]);
    if (values.length > 0) result.push({ column: match[1], values, index: match.index });
  }
  return result;
}

/** 抽出 UML 实体里的 `+type col "a/b/c"` 形式。 */
export function extractDiagrams(text) {
  const result = [];
  for (const match of text.matchAll(/\+\s*\w+\s+(\w+)\s+"([a-z_]+(?:\/[a-z_]+)+)"/g)) {
    result.push({ column: match[1], values: match[2].split('/'), index: match.index });
  }
  return result;
}

function shared(a, b) {
  const set = new Set(b);
  return a.filter((value) => set.has(value)).length;
}

/** 对单个文档做比对，返回错误列表。 */
export function inspectDocument(name, text, authority) {
  const lines = text.split('\n');
  const problems = [];
  for (const candidate of [...extractChecks(text), ...extractDiagrams(text)]) {
    const { column, values, index } = candidate;
    const lineNo = text.slice(0, index).split('\n').length - 1;
    const window = lines.slice(Math.max(0, lineNo - 4), lineNo + 3).join('\n');
    for (const object of authority.objects) {
      if (object.column !== column) continue;
      const same = object.values.length === values.length && shared(values, object.values) === values.length;
      const overlap = shared(values, object.values);
      if (same) {
        if (!ANCHOR_PATTERN.test(window)) {
          problems.push(`${name}: 完整抄了一份 ${object.key} 的取值域（${values.join('/')}），缺 \`标准对象 §${object.key}\` 锚点（第 ${lineNo + 1} 行）`);
        }
      } else if (overlap >= 2) {
        problems.push(
          `${name}: \`${column}\` 的取值与 ${object.key} 不一致（本文 ${values.join('/')}，权威 ${object.values.join('/')}，第 ${lineNo + 1} 行）`,
        );
      }
    }
  }
  for (const match of text.matchAll(/标准对象\s*§?\s*([A-Z]{2}-\d+)/g)) {
    const key = `§${match[1]}`;
    if (!KEY_PATTERN.test(key)) problems.push(`${name}: 引用键格式不合规 \`标准对象 §${match[1]}\`（只允许 ST/EN/ID/TS/P + 数字）`);
    else if (!authority.keys.includes(match[1])) problems.push(`${name}: 引用了未登记的键 \`标准对象 ${key}\``);
  }
  return problems;
}

function listDocs(dir = path.join(ROOT, 'docs')) {
  const files = [];
  const walk = (current) => {
    for (const entry of fs.readdirSync(current, { withFileTypes: true })) {
      const full = path.join(current, entry.name);
      if (entry.isDirectory()) walk(full);
      else if (entry.name.endsWith('.md')) files.push(full);
    }
  };
  walk(dir);
  return files;
}

export function run(root = ROOT) {
  const authorityPath = path.join(root, AUTHORITY);
  const authority = parseAuthority(fs.readFileSync(authorityPath, 'utf8'));
  const problems = [];
  if (authority.objects.length === 0) problems.push('标准对象.md: 没有解析到任何已展开对象的取值域');
  const files = listDocs(path.join(root, 'docs')).filter((file) => path.resolve(file) !== path.resolve(authorityPath));
  for (const file of [...files, path.join(root, 'AGENTS.md')]) {
    if (!fs.existsSync(file)) continue;
    problems.push(...inspectDocument(path.relative(root, file), fs.readFileSync(file, 'utf8'), authority));
  }
  return { problems, objects: authority.objects, keys: authority.keys };
}

if (import.meta.url === `file://${process.argv[1]}` || process.argv[1]?.endsWith('check-objects.mjs')) {
  const { problems, objects } = run();
  if (problems.length > 0) {
    console.error('标准对象门禁失败：');
    for (const problem of problems) console.error(`  - ${problem}`);
    console.error('\n修法：取值域只在 docs/标准对象.md 定义一次；删掉这里的重复枚举，改成 `标准对象 §<键>` 指针。');
    process.exit(1);
  }
  console.log(`标准对象门禁通过（${objects.length} 个已展开对象：${objects.map((o) => o.key).join(', ')}）`);
}
