#!/usr/bin/env node
// 标准对象门禁：跨域共享的取值域只允许在 docs/标准对象.md 与 docs/标准对象/ 分册里定义一次。
//
// 三条断言：
//   A 重复枚举 —— 其他文档里出现与标准对象**集合完全相同**的取值枚举时，该行或上方 3 行内必须有
//      `标准对象 §<键>` 锚点；否则报错（说明这里又抄了一份事实源）。
//   B 值集走样 —— 列名相同且与标准对象**至少共享 2 个取值**、但集合不同，视为同一枚举写歪了
//      （历史事故：账号状态在另一份文档里写成 3 值，缺 pending/banned）。
//   C 引用键 —— 任何 `标准对象 §X` 的键必须是 `ST|EN|ID|TS|P` + 数字，且在登记表里存在。
//   D YAML 围栏 —— ```yaml 代码块顶层的结构必须单一：要么全是映射键（`key:`），要么全是序列项
//      （`- `），不能混。混起来在 YAML 里是非法的，但 Markdown 渲染看不出来——历史事故：在
//      生命周期 §11.4 的 registry.yaml 序列前插了一行 `not_content_tables: [...]`，文档照常
//      渲染，谁也没发现那份"注册表"根本解析不了。
//
// 放行规则：只被一个域消费的私有枚举（与标准对象交集 < 2）不参与比对，避免把无关枚举当冲突。
import fs from 'node:fs';
import path from 'node:path';

export const AUTHORITY = 'docs/标准对象.md';
export const VOLUME_DIR = 'docs/标准对象';
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
    if (objects.some((o) => o.key === section.key)) return;
    // 派生对象不落列（可见性四档、内容档位），没有 CHECK，靠正文一行「取值域：`a` / `b`」声明
    const declarations = extractDeclarations(body);
    if (declarations.length > 0) objects.push({ key: section.key, column: null, ...declarations[0] });
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

/** 抽出正文里的「取值域：`a` / `b` / `c`」声明（派生对象用）。 */
export function extractDeclarations(text) {
  const result = [];
  for (const match of text.matchAll(/^取值域\s*[：:]\s*(.+)$/gm)) {
    const values = [...match[1].matchAll(/`([a-z0-9_]+)`/g)].map((v) => v[1]);
    if (values.length >= 2) result.push({ values, index: match.index });
  }
  return result;
}

/** 抽出「`a` / `b` / `c`」形态的连续枚举片段（散文与表格里的副本），至少三个取值才算。 */
export function extractInlineLists(text) {
  const result = [];
  const run = /`[a-z0-9_]+`(?:\s*\/\s*`[a-z0-9_]+`)+/g;
  for (const match of text.matchAll(run)) {
    const values = match[0].split('/').map((part) => part.trim().replace(/`/g, ''));
    if (values.length >= 3) result.push({ column: null, values, index: match.index });
  }
  return result;
}

/** 从所在二级章节开头到 index 之前的正文（用于「章节级锚点必须写在副本前面」）。 */
function sectionPrefix(text, index) {
  const start = text.lastIndexOf('\n## ', index) + 1;
  return text.slice(start >= 0 ? start : 0, index);
}

function shared(a, b) {
  const set = new Set(b);
  return a.filter((value) => set.has(value)).length;
}

/** 抽出 ```yaml 围栏里的顶层结构，返回 `{ kind, lines }` 序列；结构混杂时 kind = 'mixed'。 */
export function inspectYamlFences(text) {
  const problems = [];
  const fence = /^```yaml\s*$([\s\S]*?)^```\s*$/gm;
  for (const match of text.matchAll(fence)) {
    const body = match[1];
    const kinds = new Set();
    const samples = { mapping: null, sequence: null };
    for (const line of body.split('\n')) {
      if (line.trim() === '' || line.startsWith(' ') || line.startsWith('\t')) continue;
      if (line.startsWith('#') || line === '---') continue;
      if (line.startsWith('- ')) {
        kinds.add('sequence');
        samples.sequence ||= line.slice(0, 40);
      } else if (/^[A-Za-z_][\w-]*\s*:/.test(line)) {
        kinds.add('mapping');
        samples.mapping ||= line.slice(0, 40);
      }
    }
    if (kinds.size > 1) {
      const lineNo = text.slice(0, match.index).split('\n').length;
      problems.push(
        `: \`\`\`yaml 代码块顶层结构混用（映射键 \`${samples.mapping}\` 与序列项 \`${samples.sequence}\` 同级，第 ${lineNo} 行）——YAML 会解析失败；把它们分到两个键下面`,
      );
    }
  }
  return problems;
}

/** 对单个文档做比对，返回错误列表。 */
export function inspectDocument(name, text, authority) {
  const lines = text.split('\n');
  const problems = [];
  for (const candidate of [...extractChecks(text), ...extractDiagrams(text), ...extractInlineLists(text)]) {
    const { column, values, index } = candidate;
    const lineNo = text.slice(0, index).split('\n').length - 1;
    const window = lines.slice(Math.max(0, lineNo - 4), lineNo + 3).join('\n');
    const section = sectionPrefix(text, index);
    for (const object of authority.objects) {
      if (object.column !== null && object.column !== column) continue;
      const same = object.values.length === values.length && shared(values, object.values) === values.length;
      const overlap = shared(values, object.values);
      if (same) {
        if (!ANCHOR_PATTERN.test(window) && !ANCHOR_PATTERN.test(section)) {
          problems.push(`${name}: 完整抄了一份 ${object.key} 的取值域（${values.join('/')}），缺 \`标准对象 §${object.key}\` 锚点（第 ${lineNo + 1} 行）`);
        }
      } else if (overlap >= 2 && object.column !== null) {
        const subject = object.column === null ? '这处枚举' : `\`${column}\``;
        problems.push(
          `${name}: ${subject} 的取值与 ${object.key} 不一致（本文 ${values.join('/')}，权威 ${object.values.join('/')}，第 ${lineNo + 1} 行）`,
        );
      }
    }
  }
  for (const match of text.matchAll(/标准对象\s*§?\s*([A-Z]{2}-\d+)/g)) {
    const key = `§${match[1]}`;
    if (!KEY_PATTERN.test(key)) problems.push(`${name}: 引用键格式不合规 \`标准对象 §${match[1]}\`（只允许 ST/EN/ID/TS/P + 数字）`);
    else if (!authority.keys.includes(match[1])) problems.push(`${name}: 引用了未登记的键 \`标准对象 ${key}\``);
  }
  for (const problem of inspectYamlFences(text)) problems.push(`${name}${problem}`);
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

/** 权威文件 = 索引（登记表）+ 分册目录下的每一册（对象正文）。 */
export function authorityFiles(root = ROOT) {
  const files = [path.join(root, AUTHORITY)];
  const volumeDir = path.join(root, VOLUME_DIR);
  if (fs.existsSync(volumeDir)) {
    for (const name of fs.readdirSync(volumeDir).filter((n) => n.endsWith('.md')).sort()) {
      files.push(path.join(volumeDir, name));
    }
  }
  return files;
}

export function run(root = ROOT) {
  const authorities = authorityFiles(root).filter((file) => fs.existsSync(file));
  const authorityPath = path.join(root, AUTHORITY);
  const authority = parseAuthority(authorities.map((file) => fs.readFileSync(file, 'utf8')).join('\n'));
  const problems = [];
  if (authority.objects.length === 0) problems.push('标准对象.md: 没有解析到任何已展开对象的取值域');
  const excluded = new Set(authorities.map((file) => path.resolve(file)));
  const files = listDocs(path.join(root, 'docs')).filter((file) => !excluded.has(path.resolve(file)));
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
    console.error('\n修法：取值域只在 docs/标准对象*.md 定义一次；删掉这里的重复枚举，改成 `标准对象 §<键>` 指针。');
    process.exit(1);
  }
  console.log(`标准对象门禁通过（${objects.length} 个已展开对象：${objects.map((o) => o.key).join(', ')}）`);
}
