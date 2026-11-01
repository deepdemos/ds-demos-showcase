// 把 db/schema/*.sql 依次灌进本地 PostgreSQL 18（WASM 版 PGlite），任何错误都停下并打印上下文。
// 用法：npm ci && node scripts/apply-schema.mjs
// 说明：这是「人看的」应用脚本，会打印表/视图/外键/索引计数，用于核对 db/schema/README.md。
//       只需要「过/不过」的断言请用 scripts/verify-schema.mjs（CI 跑的是那个）。
import fs from 'node:fs';
import path from 'node:path';
import { PGlite } from '@electric-sql/pglite';
import { statements } from './sql-split.mjs';

const root = process.cwd();
const dir = path.join(root, 'db', 'schema');
const files = fs.readdirSync(dir).filter((f) => f.endsWith('.sql')).sort();
const db = new PGlite();

// 探测扩展可用性：90-extensions.sql 在缺扩展的环境整块跳过
const avail = new Set(
  (await db.query(`SELECT name FROM pg_available_extensions`)).rows.map((r) => r.name),
);

let total = 0;
const skipped = [];
for (const f of files) {
  const sql = fs.readFileSync(path.join(dir, f), 'utf8');
  const needs = [...sql.matchAll(/CREATE\s+EXTENSION\s+(?:IF\s+NOT\s+EXISTS\s+)?([a-z0-9_]+)/gi)].map(
    (m) => m[1],
  );
  const missing = needs.filter((n) => !avail.has(n));
  if (missing.length) {
    skipped.push(`${f}（缺扩展：${missing.join(', ')}）`);
    console.log(`skip ${f} —— 当前 PG 构建不含 ${missing.join(', ')}`);
    continue;
  }
  const sts = statements(sql);
  for (const s of sts) {
    try {
      await db.exec(s);
      total++;
    } catch (e) {
      console.error(`\nFAIL ${f}\n${s.split('\n').slice(0, 12).join('\n')}\n→ ${e.message}`);
      process.exit(1);
    }
  }
  console.log(`ok ${f} (${sts.length} stmts)`);
}

const one = async (q) => (await db.query(q)).rows[0];
const t = await one(
  `SELECT count(*)::int AS n FROM information_schema.tables WHERE table_schema='public' AND table_type='BASE TABLE'`,
);
const v = await one(
  `SELECT count(*)::int AS n FROM information_schema.views WHERE table_schema='public'`,
);
const fk = await one(`SELECT count(*)::int AS n FROM pg_constraint WHERE contype='f'`);
const ix = await one(`SELECT count(*)::int AS n FROM pg_indexes WHERE schemaname='public'`);
const ver = await one('SHOW server_version');
console.log(`\n=== applied ${total} statements ===`);
console.log(`tables=${t.n} views=${v.n} fks=${fk.n} indexes=${ix.n}`);
console.log(`server_version=${ver.server_version}`);
if (skipped.length) console.log(`skipped: ${skipped.join(' / ')}`);
