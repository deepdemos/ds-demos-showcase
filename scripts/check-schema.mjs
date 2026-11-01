// 表结构门禁：db/schema 是唯一事实源，文档不许再手写 DDL。
// 纯静态（零依赖），CI 与本地都能跑：node scripts/check-schema.mjs
import fs from 'node:fs';
import path from 'node:path';
import { pathToFileURL } from 'node:url';

const ACT_SRC = '(?:\\s+ON\\s+(?:DELETE|UPDATE)\\s+(?:CASCADE|RESTRICT|SET NULL|SET DEFAULT|NO ACTION))*';
const REF = new RegExp('REFERENCES\\s+([a-z_]+)\\s*\\(\\s*([^)]*?)\\s*\\)(' + ACT_SRC + ')', 'i');

// 文档白名单：明确以「旧栈现状 / 历史快照」为题的，允许抄旧 DDL（它们是史料，不是目标态）
const DOC_ALLOW = [
  /^docs\/.*旧栈.*\.md$/,
  /^docs\/ORM现状与演进\.md$/,
  /^docs\/目标设计\/.*\.md$/, // 目标设计目录里的 DDL 也要求引用；这条只是占位，下面用 NEEDS 覆盖
];
const DOC_ALLOW_REAL = [
  /^docs\/.*旧栈.*\.md$/,
  /^docs\/ORM现状与演进\.md$/,
];

function readDocs(root) {
  const out = [];
  const walk = (dir) => {
    for (const e of fs.readdirSync(dir, { withFileTypes: true })) {
      const p = path.join(dir, e.name);
      if (e.isDirectory()) { walk(p); continue; }
      if (!e.name.endsWith('.md')) continue;
      out.push({ rel: path.relative(root, p).split(path.sep).join('/'), text: fs.readFileSync(p, 'utf8') });
    }
  };
  walk(path.join(root, 'docs'));
  return out;
}

function parseSchema(root) {
  const dir = path.join(root, 'db', 'schema');
  if (!fs.existsSync(dir)) return { error: 'db/schema 不存在' };
  const files = fs.readdirSync(dir).filter((f) => f.endsWith('.sql')).sort();
  const tables = new Map(); // name -> { file, cols:Set, hasPk, body }
  const views = new Map();
  const indexes = [];
  const problems = [];
  const fileNos = [];

  for (const f of files) {
    const m = f.match(/^(\d\d)-[a-z0-9-]+\.sql$/);
    if (!m) { problems.push(`${f}：文件名必须是 NN-<name>.sql`); continue; }
    fileNos.push(Number(m[1]));
    const text = fs.readFileSync(path.join(dir, f), 'utf8').split('\r').join('');
    for (const st of text.split(/;\s*\n/)) {
      const body = st.trim();
      if (!body) continue;
      const ct = body.match(/CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?([a-z_]+)\s*\(([\s\S]*)\)\s*$/i);
      if (ct) {
        const name = ct[1];
        if (tables.has(name)) problems.push(`${f}：表 ${name} 重复定义（另一处在 ${tables.get(name).file}）`);
        const cols = new Set();
        for (const line of ct[2].split('\n')) {
          const t = line.trim();
          if (/^(CONSTRAINT|PRIMARY\s+KEY|UNIQUE|CHECK|FOREIGN\s+KEY|--)/i.test(t)) continue;
          const c = (t.match(/^([a-z_]+)\s+/i) || [])[1];
          if (c) cols.add(c);
        }
        const hasPk = /PRIMARY\s+KEY/i.test(ct[2]);
        tables.set(name, { file: f, cols, hasPk, body });
        continue;
      }
      const cv = body.match(/CREATE\s+(?:OR\s+REPLACE\s+)?VIEW\s+([a-z_]+)\s+AS\s+([\s\S]*)$/i);
      if (cv) {
        if (views.has(cv[1])) problems.push(`${f}：视图 ${cv[1]} 重复定义`);
        views.set(cv[1], { file: f, body: cv[2] });
        continue;
      }
      const ci = body.match(/CREATE\s+(?:UNIQUE\s+)?INDEX\s+([a-z_][a-z0-9_]*)\s+ON\s+([a-z_]+)/i);
      if (ci) { indexes.push({ name: ci[1], table: ci[2], file: f }); continue; }
      if (/^CREATE\s+(TABLE|VIEW|INDEX)/i.test(body) === false && /^ALTER\s+TABLE/i.test(body)) continue;
    }
  }

  // 前缀连续
  fileNos.sort((a, b) => a - b);
  for (let i = 0; i < fileNos.length; i++) {
    const expect = 10 * (i + 1);
    if (fileNos[i] !== expect) { problems.push(`db/schema 前缀不连续：第 ${i + 1} 个文件前缀是 ${fileNos[i]}，应为 ${expect}`); break; }
  }

  // 每表必须有主键
  for (const [name, t] of tables) if (!t.hasPk) problems.push(`表 ${name}（${t.file}）没有 PRIMARY KEY`);

  // 建表文件里不许出现外键（集中在 80-late-fk.sql）
  for (const f of files) {
    if (f === '80-late-fk.sql') continue;
    const text = fs.readFileSync(path.join(dir, f), 'utf8').split('\r').join('');
    for (const st of text.split(/;\s*\n/)) {
      if (!/CREATE\s+TABLE/i.test(st)) continue;
      if (/REFERENCES\s+[a-z_]/i.test(st) || /^\s*FOREIGN\s+KEY/im.test(st)) {
        problems.push(`${f}：建表语句里出现外键，必须搬到 80-late-fk.sql`);
        break;
      }
    }
  }

  // 80-late-fk.sql：每条外键的表/列/目标列都必须真实存在
  const fkFile = path.join(dir, '80-late-fk.sql');
  const fkCount = { value: 0 };
  if (fs.existsSync(fkFile)) {
    const text = fs.readFileSync(fkFile, 'utf8').split('\r').join('');
    for (const st of text.split(/;\s*\n/)) {
      const m = st.match(/ALTER\s+TABLE\s+([a-z_]+)\s+ADD\s+CONSTRAINT\s+([a-z_0-9]+)\s+FOREIGN\s+KEY\s*\(\s*([^)]*?)\s*\)\s*REFERENCES\s+([a-z_]+)\s*\(\s*([^)]*?)\s*\)/i);
      if (!m) { if (/ALTER\s+TABLE/i.test(st)) problems.push('80-late-fk.sql 有无法解析的语句：' + st.slice(0, 80)); continue; }
      fkCount.value++;
      const [, tbl, cname, cols, target, tcols] = m;
      const t = tables.get(tbl);
      if (!t) { problems.push(`外键 ${cname}：表 ${tbl} 不存在`); continue; }
      const cc = cols.split(',').map((s) => s.trim()).filter(Boolean);
      const tc = tcols.split(',').map((s) => s.trim()).filter(Boolean);
      if (cc.length !== tc.length) problems.push(`外键 ${cname}：列数不匹配`);
      for (const c of cc) if (!t.cols.has(c)) problems.push(`外键 ${cname}：${tbl}.${c} 不存在`);
      const tt = tables.get(target);
      if (!tt) { problems.push(`外键 ${cname}：目标表 ${target} 不存在`); continue; }
      for (const c of tc) if (!tt.cols.has(c)) problems.push(`外键 ${cname}：${target}.${c} 不存在`);
    }
  }

  // 索引不重复、且建在存在的表上
  const seen = new Map();
  for (const ix of indexes) {
    if (seen.has(ix.name)) problems.push(`索引 ${ix.name} 重复（${seen.get(ix.name)} / ${ix.file}）`);
    seen.set(ix.name, ix.file);
    if (!tables.has(ix.table)) problems.push(`索引 ${ix.name} 建在不存在的表 ${ix.table} 上`);
  }

  // 视图名不得与表名冲突，且引用的表必须存在
  for (const [name, v] of views) {
    if (tables.has(name)) problems.push(`视图 ${name} 与表同名`);
    const refs = new Set();
    for (const m of v.body.matchAll(/\b(?:FROM|JOIN)\s+([a-z_][a-z0-9_]*)/gi)) refs.add(m[1]);
    for (const r of refs) if (!tables.has(r) && !views.has(r)) problems.push(`视图 ${name} 引用了不存在的 ${r}`);
  }

  return { files, tables, views, indexes, problems, fkCount: fkCount.value };
}

export function inspectSchema(root) {
  const problems = [];
  const s = parseSchema(root);
  if (s.error) return [s.error];
  problems.push(...s.problems);

  // 文档不许手写 DDL
  const docs = readDocs(root);
  const ddlInDoc = [];
  for (const d of docs) {
    if (DOC_ALLOW_REAL.some((re) => re.test(d.rel))) continue;
    for (const block of d.text.matchAll(/```sql\r?\n([\s\S]*?)```/g)) {
      const body = block[1];
      const lines = body.split('\n');
      for (const line of lines) {
        if (!/CREATE\s+(TABLE|VIEW|INDEX)/i.test(line)) continue;
        // 迁移章节的占位写法 `CREATE TABLE x (...)` 允许
        if (/CREATE\s+TABLE\s+[a-z_]+\s*\(\s*(\.\.\.|…)\s*\)/i.test(line)) continue;
        ddlInDoc.push(`${d.rel}: ${line.trim().slice(0, 70)}`);
      }
    }
  }
  if (ddlInDoc.length) {
    problems.push(`文档里仍手写 DDL（共 ${ddlInDoc.length} 处）——应改为引用 db/schema/*.sql：`);
    for (const x of ddlInDoc.slice(0, 12)) problems.push('  ' + x);
  }

  // 文档引用的 schema 文件必须存在
  const known = new Set(s.files);
  const referenced = new Set();
  for (const d of docs) {
    for (const m of d.text.matchAll(/db\/schema\/(\d\d-[a-z0-9-]+\.sql)/g)) {
      referenced.add(m[1]);
      if (!known.has(m[1])) problems.push(`${d.rel} 引用了不存在的 db/schema/${m[1]}`);
    }
  }
  // 每个建表文件至少被一份文档引用（新域不能只加 SQL 不挂说明）
  for (const f of s.files) {
    if (f === '80-late-fk.sql' || f === 'README.md') continue;
    if (!referenced.has(f)) problems.push(`db/schema/${f} 没有被任何文档引用（加一句「DDL 见 db/schema/${f}」）`);
  }
  return problems;
}

export function stats(root) {
  const s = parseSchema(root);
  return { tables: s.tables ? s.tables.size : 0, views: s.views ? s.views.size : 0, fks: s.fkCount || 0, files: s.files ? s.files.length : 0 };
}

export function run(root = process.cwd()) {
  const problems = inspectSchema(root);
  const st = stats(root);
  if (problems.length) {
    console.error(`表结构门禁失败（${problems.length} 条）：`);
    for (const p of problems) console.error('  - ' + p);
    return 1;
  }
  console.log(`表结构门禁通过（${st.files} 个文件 / ${st.tables} 表 / ${st.views} 视图 / ${st.fks} 外键；文档已无手写 DDL）`);
  return 0;
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) process.exit(run());
