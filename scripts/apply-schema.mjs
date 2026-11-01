// 把 db/schema/*.sql 依次灌进本地 PostgreSQL 18（WASM 版 PGlite），任何错误都停下并打印上下文。
// 用法：node scripts/apply-schema.mjs
// 依赖（仅本地）：npm i -D @electric-sql/pglite
// 说明：这是「真库验一次」的脚本；CI 里跑的是纯静态门禁 scripts/check-schema.mjs（不引入运行时依赖）。
import fs from 'node:fs';
import path from 'node:path';
import { createRequire } from 'node:module';

const require = createRequire(import.meta.url);
let PGlite;
try {
  ({ PGlite } = require('@electric-sql/pglite'));
} catch {
  // 回退：本机临时安装位置（可用 PGLITE_PATH 覆盖）
  const fallback = process.env.PGLITE_PATH
    || 'file:///C:/Users/ROG/AppData/Local/Temp/pgq15/node_modules/@electric-sql/pglite/dist/index.js';
  ({ PGlite } = await import(fallback));
}

const root = process.cwd();
const dir = path.join(root, 'db', 'schema');
const files = fs.readdirSync(dir).filter((f) => f.endsWith('.sql')).sort();
const db = new PGlite();

function statements(sql) {
  return sql
    .split(/;\s*\n/)
    .map((s) => s.trim())
    .filter((s) => s && !/^--/.test(s.split('\n').find((l) => l.trim() && !/^\s*--/.test(l)) || ''));
}

// 探测扩展可用性：90-extensions.sql 在缺扩展的环境整块跳过
const avail = new Set(
  (await db.query(`SELECT name FROM pg_available_extensions`)).rows.map((r) => r.name)
);

let total = 0;
let skipped = [];
for (const f of files) {
  const sql = fs.readFileSync(path.join(dir, f), 'utf8');
  const needs = [...sql.matchAll(/CREATE\s+EXTENSION\s+(?:IF\s+NOT\s+EXISTS\s+)?([a-z0-9_]+)/gi)].map((m) => m[1]);
  const missing = needs.filter((n) => !avail.has(n));
  if (missing.length) {
    skipped.push(`${f}（缺扩展：${missing.join(', ')}）`);
    console.log(`skip ${f} —— 当前 PG 构建不含 ${missing.join(', ')}`);
    continue;
  }
  for (const s of statements(sql)) {
    const body = s.split('\n').filter((l) => !/^\s*--/.test(l)).join('\n').trim();
    if (!body) continue;
    try {
      await db.exec(body + ';');
      total++;
    } catch (e) {
      console.error(`\nFAIL ${f}\n${body.split('\n').slice(0, 12).join('\n')}\n→ ${e.message}`);
      process.exit(1);
    }
  }
  console.log(`ok ${f} (${statements(sql).length} stmts)`);
}

const one = async (q) => (await db.query(q)).rows[0];
const t = await one(`SELECT count(*)::int AS n FROM information_schema.tables WHERE table_schema='public' AND table_type='BASE TABLE'`);
const v = await one(`SELECT count(*)::int AS n FROM information_schema.views WHERE table_schema='public'`);
const fk = await one(`SELECT count(*)::int AS n FROM pg_constraint WHERE contype='f'`);
const ix = await one(`SELECT count(*)::int AS n FROM pg_indexes WHERE schemaname='public'`);
const ver = await one('SHOW server_version');
console.log(`\n=== applied ${total} statements ===`);
console.log(`tables=${t.n} views=${v.n} fks=${fk.n} indexes=${ix.n}`);
console.log(`server_version=${ver.server_version}`);
if (skipped.length) console.log(`skipped: ${skipped.join(' / ')}`);
