// 迁移门禁：命名、序号、校验和。
//
// 纯静态（零依赖），CI 与本地都能跑：
//   node scripts/check-migrations.mjs           # 校验
//   node scripts/check-migrations.mjs --seal    # 加/删了文件之后重新生成校验清单
//
// 它抓的是 migration 最经典的翻车方式：**改了已经合入的迁移**。
// 你本地改完重跑没感觉，别人的库已经跑过那一条，永远拿不到这次修改——结构从此悄悄分叉。
import fs from 'node:fs';
import path from 'node:path';
import crypto from 'node:crypto';
import { pathToFileURL } from 'node:url';

const BASELINE_DIR = 'db/schema';
const MIGRATIONS_DIR = 'db/migrations';
const MANIFEST = 'db/migrations/checksums.json';

function hash(file) {
  return crypto.createHash('sha256').update(fs.readFileSync(file)).digest('hex');
}

function listSql(dir) {
  if (!fs.existsSync(dir)) return [];
  return fs
    .readdirSync(dir)
    .filter((f) => f.endsWith('.sql'))
    .sort();
}

/** 迁移文件必须是 NNNN_名称.sql，且序号从 0001 连续。 */
export function inspectNames(migrationFiles) {
  const problems = [];
  const versions = [];
  for (const f of migrationFiles) {
    const m = f.match(/^(\d{4})_([a-z0-9_]+)\.sql$/);
    if (!m) {
      problems.push(`${f}：文件名必须是 NNNN_名称.sql（四位序号 + 下划线 + 小写下划线名）`);
      continue;
    }
    versions.push({ file: f, version: Number(m[1]) });
  }
  versions.sort((a, b) => a.version - b.version);
  versions.forEach((v, i) => {
    const want = i + 1;
    if (v.version !== want) {
      problems.push(`序号不连续：第 ${i + 1} 个迁移是 ${String(v.version).padStart(4, '0')}，应为 ${String(want).padStart(4, '0')}（${v.file}）`);
    }
  });
  return problems;
}

/** 校验和与清单比对；manifest 缺失时提示 seal。 */
export function inspectChecksums(root, files, manifest) {
  if (!manifest) return ['缺少 db/migrations/checksums.json——先跑 node scripts/check-migrations.mjs --seal'];
  const problems = [];
  for (const f of files) {
    const rel = f.path;
    if (!(rel in manifest)) {
      problems.push(`${rel} 不在 checksums.json 里——跑 node scripts/check-migrations.mjs --seal`);
      continue;
    }
    if (manifest[rel] !== f.sum) {
      problems.push(`${rel} 的内容变了（校验和对不上）。已合入的迁移不许改，请新增一条`);
    }
  }
  for (const rel of Object.keys(manifest)) {
    if (!files.some((f) => f.path === rel)) {
      problems.push(`${rel} 在 checksums.json 里但文件不存在`);
    }
  }
  return problems;
}

function collect(root) {
  const out = [];
  for (const dir of [BASELINE_DIR, MIGRATIONS_DIR]) {
    for (const name of listSql(path.join(root, dir))) {
      const rel = path.posix.join(dir, name);
      out.push({ path: rel, sum: hash(path.join(root, dir, name)) });
    }
  }
  return out;
}

export function inspectMigrations(root) {
  const problems = [];
  const baseline = listSql(path.join(root, BASELINE_DIR));
  const migrationFiles = listSql(path.join(root, MIGRATIONS_DIR));

  if (baseline.length === 0) problems.push(`${BASELINE_DIR} 是空的——基线不能删，它等价于「0000 号迁移」`);

  problems.push(...inspectNames(migrationFiles));

  const manifestPath = path.join(root, MANIFEST);
  let manifest = null;
  if (fs.existsSync(manifestPath)) {
    try {
      manifest = JSON.parse(fs.readFileSync(manifestPath, 'utf8'));
    } catch (e) {
      problems.push(`checksums.json 不是合法 JSON：${e.message}`);
    }
  }
  problems.push(...inspectChecksums(root, collect(root), manifest));
  return problems;
}

function seal(root) {
  const files = collect(root);
  const manifest = {};
  for (const f of files) manifest[f.path] = f.sum;
  fs.writeFileSync(path.join(root, MANIFEST), JSON.stringify(manifest, null, 2) + '\n');
  console.log(`已生成 ${MANIFEST}（${files.length} 个文件：基线 ${listSql(path.join(root, BASELINE_DIR)).length} + 迁移 ${listSql(path.join(root, MIGRATIONS_DIR)).length}）`);
}

export function run(root = process.cwd()) {
  const problems = inspectMigrations(root);
  if (problems.length) {
    console.error(`迁移门禁失败（${problems.length} 条）：`);
    for (const p of problems) console.error('  - ' + p);
    return 1;
  }
  const mig = listSql(path.join(root, MIGRATIONS_DIR)).length;
  console.log(`迁移门禁通过（基线 ${listSql(path.join(root, BASELINE_DIR)).length} 个文件 + ${mig} 条迁移，校验和一致）`);
  return 0;
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  const root = process.cwd();
  if (process.argv.includes('--seal')) {
    seal(root);
    process.exit(0);
  }
  process.exit(run(root));
}
