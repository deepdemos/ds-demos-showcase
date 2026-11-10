#!/usr/bin/env node
// 作品形态文档门禁：`docs/作品形态接入清单.md` 必须与代码、与数据库清单一致。
//
// 为什么需要它：形态这件事有三份东西同时描述——`shapes.go` 里的词汇、
// `demo_type_formats` 里的类型清单、文档里给作者看的表格。三者漂开之后，
// 作者会照着文档投一个平台根本不收的格式，或者平台支持了一种格式却没人知道。
//
// 这道门禁查三件事：
//   1. 代码认识的每一种格式，文档里都要写到；
//   2. 数据库里每一对（类型, 格式），文档里都要出现在那个类型名下；
//   3. 文档里不许出现代码不认识的格式——写错了比漏了更坏，作者会照着做。
//
// 它不查：入口文件怎么放、目录结构对不对（那是散文，只能人看）。
import fs from 'node:fs';
import path from 'node:path';
import { pathToFileURL } from 'node:url';

export const SHAPES_GO = 'backend/internal/domain/demo/shapes.go';
export const MIGRATIONS_DIR = 'db/migrations';
export const DOC = 'docs/作品形态接入清单.md';

// 表格一行的形态：`| web | single:html、zip:html | ... |`
// 第一列是类型码，后面可能跟中文名（`web（网页作品）`），所以要允许列内有多余文字。
const TYPE_ROW = /^\|\s*`?([a-z][a-z0-9_]*)`?[^|]*\|(.*)$/gm;

// parseCode 读出代码认识的格式集合。
export function parseCode(source) {
  const kinds = new Set();
  const block = source.match(/var singleFileKinds = map\[string\]string\{([\s\S]*?)\n\}/);
  if (!block) throw new Error('没有在 shapes.go 里找到 singleFileKinds');
  for (const m of block[1].matchAll(/"\.[a-z0-9]+":\s*"([a-z0-9]+)"/g)) {
    kinds.add('single:' + m[1]);
  }
  const zipEntry = source.match(/const zipEntry = "([^"]+)"/);
  if (!zipEntry) throw new Error('没有在 shapes.go 里找到 zipEntry');

  const formats = new Set(kinds);
  formats.add('url');
  formats.add('zip:html');
  if (/\.pck/.test(source)) formats.add('zip:godot');
  return formats;
}

// parseMigrations 读出 `demo_type_formats` 里登记的（类型, 格式）对。
export function parseMigrations(dir) {
  const pairs = new Map();
  const files = fs.readdirSync(dir).filter((name) => name.endsWith('.sql')).sort();
  for (const name of files) {
    const sql = fs.readFileSync(path.join(dir, name), 'utf8');
    const re = /\('([a-z][a-z0-9_]*)',\s*'(single:[a-z0-9]+|zip:[a-z0-9]+|url)'/g;
    for (const m of sql.matchAll(re)) {
      if (!pairs.has(m[1])) pairs.set(m[1], new Set());
      pairs.get(m[1]).add(m[2]);
    }
  }
  return pairs;
}

// parseDoc 读出文档每行声称的（类型, 格式）。
export function parseDoc(text) {
  const rows = new Map();
  const claimed = new Set();
  for (const m of text.matchAll(TYPE_ROW)) {
    const type = m[1];
    const cells = m[2];
    const formats = [...cells.matchAll(/`(single:[a-z0-9]+|zip:[a-z0-9]+|url)`/g)].map((x) => x[1]);
    if (formats.length === 0) continue;
    if (!rows.has(type)) rows.set(type, new Set());
    for (const f of formats) {
      rows.get(type).add(f);
      claimed.add(f);
    }
  }
  return { rows, claimed };
}

export function checkShapeDocs({ code, migrations, doc }) {
  const problems = [];
  const { rows, claimed } = parseDoc(doc);

  const allFormats = new Set();
  for (const formats of migrations.values()) for (const f of formats) allFormats.add(f);
  for (const f of code) allFormats.add(f);

  for (const format of [...allFormats].sort()) {
    if (!claimed.has(format)) problems.push(`文档里没写到格式 \`${format}\``);
  }
  for (const format of [...claimed].sort()) {
    if (!allFormats.has(format)) {
      problems.push(`文档写了代码不认识的格式 \`${format}\``);
    }
  }
  for (const [type, formats] of [...migrations.entries()].sort()) {
    const documented = rows.get(type) ?? new Set();
    for (const format of [...formats].sort()) {
      if (!documented.has(format)) {
        problems.push(`类型 \`${type}\` 在数据库里收 \`${format}\`，文档那一行没写`);
      }
    }
  }
  const emptyTypes = [...rows.entries()].filter(([, set]) => set.size === 0).map(([t]) => t);
  if (emptyTypes.length > 0) {
    problems.push(`这些类型的格式列是空的，但表格里不该有这种行：${emptyTypes.join('、')}`);
  }
  return { ok: problems.length === 0, problems };
}

function main() {
  const code = fs.readFileSync(SHAPES_GO, 'utf8');
  const migrations = parseMigrations(MIGRATIONS_DIR);
  const doc = fs.readFileSync(DOC, 'utf8');
  const { ok, problems } = checkShapeDocs({ code: parseCode(code), migrations, doc });
  if (!ok) {
    console.error('作品形态文档门禁失败：');
    for (const problem of problems) console.error(`  - ${problem}`);
    console.error('处理指引：改 docs/作品形态接入清单.md，让表格与代码、与 demo_type_formats 对齐。');
    process.exit(1);
  }
  const formats = parseCode(code).size;
  console.log(`作品形态文档门禁通过（${formats} 种格式，${migrations.size} 个类型，表格一致）`);
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  main();
}
