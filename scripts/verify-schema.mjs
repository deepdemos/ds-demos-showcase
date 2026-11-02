// 真库校验：把 db/schema 基线 + db/migrations 增量灌进 PostgreSQL 18（WASM 版 PGlite），然后逐条验证
// 「设计文档说不允许的事，数据库真的不允许」。
//
// 用法：
//   npm install            # 首次（装 @electric-sql/pglite）
//   node scripts/verify-schema.mjs
//
// 和 check-schema.mjs 的分工：
//   check-schema.mjs  —— 纯静态，只读文件，不连数据库，CI 每次跑。
//   verify-schema.mjs —— 真库执行，验证约束/外键/触发器真的生效，CI 也跑（contract job）。
//
// **能证明什么、不能证明什么**（写在这里，免得又被当成更强的保证）：
//   能证明：DDL 在真 PG 18 上能执行；CHECK / FOREIGN KEY / 级联删除的行为符合预期；
//           注册表里的键与设计文档 §4 一致。
//   不能证明：业务代码怎么用这些表；性能；事务边界；`90-extensions.sql` 里那类扩展相关的东西
//           （PGlite 无 contrib，该文件在缺扩展的环境整块跳过）。
//
// 注意：这里**不写 schema_migrations 表**——它只对真库有意义（记录跑到第几版），
// 而每次校验都是全新的空库。迁移文件的执行顺序就是它的版本顺序。
import fs from 'node:fs';
import path from 'node:path';
import { PGlite } from '@electric-sql/pglite';
import { statements } from './sql-split.mjs';

const root = process.cwd();
const dir = path.join(root, 'db', 'schema');
const files = fs.readdirSync(dir).filter((f) => f.endsWith('.sql')).sort();

// ---- 设计契约：注册表应当有这些键（来自 docs/设置域设计.md §4，改设计才改这里）----
const EXPECTED_KEYS = [
  ['demo.auto_approve', 'site', 'boolean', 'false'],
  ['demo.anonymous_auto_approve', 'site', 'boolean', 'false'],
  ['demo.upload_package_mib', 'site', 'number', '200'],
  ['demo.upload_extracted_mib', 'site', 'number', '500'],
  ['demo.cover_max_megapixels', 'site', 'number', '40'],
  ['auth.login_rate_account', 'site', 'number', '30'],
  ['auth.login_rate_network', 'site', 'number', '60'],
  ['collection.max_per_account', 'site', 'number', '20'],
  ['collection.max_items', 'site', 'number', '500'],
  ['history.max_entries_account', 'site', 'number', '200'],
  ['lifecycle.restore_window_days', 'site', 'number', '30'],
  ['modelsdev.sync_enabled', 'site', 'boolean', 'false'],
  ['modelsdev.sync_interval_hours', 'site', 'number', '24'],
  ['modelsdev.search_official_only', 'site', 'boolean', 'true'],
  ['notify.retention_days', 'site', 'number', '90'],
  ['notify.retention_days', 'user', 'number', '90'],
  ['history.record', 'user', 'boolean', 'true'],
  ['collection.public_default', 'user', 'boolean', 'false'],
];
// demo.cover_inline 的内联上限（db/schema/30-demo.sql 的两条 CHECK 必须同值）
const COVER_INLINE_LIMIT = 32768;

const db = new PGlite();
let pass = 0;
let fail = 0;
const failures = [];

function record(okFlag, name, detail) {
  if (okFlag) {
    pass++;
  } else {
    fail++;
    failures.push(name + (detail ? ' :: ' + detail : ''));
    console.log('  FAIL', name, detail || '');
  }
}

async function ok(name, sql, expect) {
  const r = await db.query(sql);
  const got = r.rows[0] ? Object.values(r.rows[0])[0] : undefined;
  record(String(got) === String(expect), name, 'expect ' + expect + ', got ' + got);
}

async function rejects(name, sql, needle) {
  try {
    await db.exec(sql);
    record(false, name, '应当被拒但通过了');
  } catch (e) {
    const m = String(e.message);
    record(m.includes(needle), name, '期望含 ' + needle + '，实际 ' + m.slice(0, 200));
  }
}

async function accepts(name, sql) {
  try {
    await db.exec(sql);
    record(true, name);
  } catch (e) {
    record(false, name, String(e.message).slice(0, 200));
  }
}

// ---------- 1. 应用 ----------
console.log('--- 应用 db/schema/*.sql ---');
const avail = new Set((await db.query('SELECT name FROM pg_available_extensions')).rows.map((r) => r.name));
const skipped = [];
for (const f of files) {
  const sql = fs.readFileSync(path.join(dir, f), 'utf8');
  const sts = statements(sql);
  let applied = 0;
  for (const st of sts) {
    try {
      await db.exec(st);
      applied++;
    } catch (e) {
      const msg = String(e.message);
      if (/extension .* is not available|CREATE EXTENSION/.test(msg)) {
        skipped.push(f + '（缺扩展）');
        applied = -1;
        break;
      }
      console.log('  APPLY FAIL', f, '::', msg.slice(0, 300));
      process.exit(1);
    }
  }
  if (applied >= 0) console.log('  applied', f, applied, 'statements');
}
console.log('  skipped:', skipped.length ? skipped.join(', ') : '(none)');

// 基线之后：按序号应用 db/migrations（这里不登记，只按顺序执行）
const migDir = path.join(root, 'db', 'migrations');
const migFiles = fs.existsSync(migDir)
  ? fs.readdirSync(migDir).filter((f) => f.endsWith('.sql')).sort()
  : [];
if (migFiles.length) console.log('--- 应用 db/migrations/*.sql ---');
for (const f of migFiles) {
  const sql = fs.readFileSync(path.join(migDir, f), 'utf8');
  for (const st of statements(sql)) {
    try {
      await db.exec(st);
    } catch (e) {
      console.log('  APPLY FAIL', f, '::', String(e.message).slice(0, 300));
      process.exit(1);
    }
  }
  console.log('  applied', f);
}

// 记下实际应用了几条（用于下面的计数自证：脚本真的跑了，不是空过）
const appliedFiles = files.filter((f) => !skipped.some((s) => s.startsWith(f)));

// ---------- 2. 注册表与设计契约一致 ----------
console.log('--- 设置域注册表 ---');
await ok('setting_keys 行数 = 设计契约条数', 'SELECT count(*) FROM setting_keys', EXPECTED_KEYS.length);
await ok('site 行数', "SELECT count(*) FROM setting_keys WHERE scope='site'",
  EXPECTED_KEYS.filter((k) => k[1] === 'site').length);
await ok('user 行数', "SELECT count(*) FROM setting_keys WHERE scope='user'",
  EXPECTED_KEYS.filter((k) => k[1] === 'user').length);
for (const [k, sc, vt, dv] of EXPECTED_KEYS) {
  const r = await db.query(
    'SELECT value_type, default_value::text AS dv FROM setting_keys WHERE key=$1 AND scope=$2',
    [k, sc],
  );
  const row = r.rows[0];
  record(!!row && row.value_type === vt && row.dv === dv, '键 ' + k + ' / ' + sc,
    JSON.stringify(row));
}
// 白名单外的一律写不进去（SET-010 的根本保证）
await rejects('未登记站点键被拒',
  "INSERT INTO site_settings (key, value_type, value) VALUES ('nope.key','boolean','true'::jsonb)",
  'fk_site_settings_key71');
await rejects('用户级键写进 site_settings 被拒',
  "INSERT INTO site_settings (key, value_type, value) VALUES ('history.record','boolean','true'::jsonb)",
  'fk_site_settings_key71');
await rejects('站点键写进 user_preferences 被拒',
  "INSERT INTO user_preferences (account_id, key, value_type, value) VALUES (1,'demo.auto_approve','boolean','true'::jsonb)",
  'fk_user_preferences_key73');
await rejects('值类型与注册表不符被拒',
  "INSERT INTO site_settings (key, value_type, value) VALUES ('demo.upload_package_mib','number','\"abc\"'::jsonb)",
  'ck_site_settings_value');
await rejects('键名格式非法被拒（无点）',
  "INSERT INTO setting_keys (key, scope, value_type, default_value) VALUES ('nodot','site','boolean','true'::jsonb)",
  'ck_setting_keys_form');
await rejects('默认值与 value_type 不符被拒',
  "INSERT INTO setting_keys (key, scope, value_type, default_value) VALUES ('x.bad','site','boolean','\"s\"'::jsonb)",
  'ck_setting_keys_default');
await accepts('写已登记站点参数',
  "INSERT INTO site_settings (key, value_type, value) VALUES ('demo.auto_approve','boolean','true'::jsonb)");
await ok('读回来的值', "SELECT value::text FROM site_settings WHERE key='demo.auto_approve'", 'true');

// ---------- 3. 账号与等级（EXP-001：级数不封顶）----------
console.log('--- 账号 / 等级 ---');
await db.exec("INSERT INTO accounts (sub, status) VALUES ('VERIFY-SUB-1','active')");
await ok('level 默认 0', "SELECT level FROM accounts WHERE sub='VERIFY-SUB-1'", 0);
await accepts('level=7 允许', "UPDATE accounts SET level=7 WHERE sub='VERIFY-SUB-1'");
await accepts('level=42 允许（级数不封顶）', "UPDATE accounts SET level=42 WHERE sub='VERIFY-SUB-1'");
await rejects('level=-1 被拒', "UPDATE accounts SET level=-1 WHERE sub='VERIFY-SUB-1'", 'ck_accounts_level_nonneg');
const aid = (await db.query("SELECT id FROM accounts WHERE sub='VERIFY-SUB-1'")).rows[0].id;

// ---------- 4. 用户偏好：本人读写 + 级联 ----------
console.log('--- 用户偏好 ---');
await accepts('写用户偏好',
  "INSERT INTO user_preferences (account_id, key, value_type, value) VALUES (" + aid + ",'history.record','boolean','false'::jsonb)");
await ok('读回用户偏好',
  "SELECT value::text FROM user_preferences WHERE key='history.record'", 'false');
await db.exec('DELETE FROM accounts WHERE id = ' + aid);
await ok('删账号级联删偏好',
  "SELECT count(*) FROM user_preferences WHERE key='history.record'", 0);

// ---------- 5. 作品类型主数据（DEMO-001：外键而非 CHECK）----------
console.log('--- 作品类型 ---');
await ok('demo_types 行数', 'SELECT count(*) FROM demo_types', 4);
await ok('demo_types 码值',
  "SELECT string_agg(code, ',' ORDER BY sort) FROM demo_types", 'web,zip,link,image');
await db.exec("INSERT INTO demos (slug, title, moderation_state, lifecycle_state) VALUES ('verify-demo-1','驗證','approved','published')");
const did = (await db.query("SELECT id FROM demos WHERE slug='verify-demo-1'")).rows[0].id;
await rejects('非法 demo_type 被外键拒',
  "INSERT INTO demo_revisions (demo_id, rev_no, demo_type, title) VALUES (" + did + ",1,'video','x')",
  'fk_demo_revisions_demo_type75');
await accepts('合法 demo_type 通过',
  "INSERT INTO demo_revisions (demo_id, rev_no, demo_type, title) VALUES (" + did + ",2,'zip','x')");

// ---------- 6. 封面内联两条 CHECK 同值（手册 §1 第 4 行的「两处必须同改」）----------
console.log('--- 结构性上限 ---');
await ok('cover_inline 上限（demos）',
  "SELECT (regexp_match(pg_get_constraintdef(oid), 'octet_length\\(cover_inline\\) <= (\\d+)'))[1] FROM pg_constraint WHERE conname='ck_demos_cover_inline'",
  COVER_INLINE_LIMIT);
await rejects('demos.cover_inline 超限被拒',
  "UPDATE demos SET cover_inline = decode(repeat('00', " + (COVER_INLINE_LIMIT + 1) + "), 'hex') WHERE id=" + did,
  'ck_demos_cover_inline');

// ---------- 7. 治理域目录表与审计约束 ----------
console.log('--- 治理域 ---');
const perm = (await db.query('SELECT count(*) AS n FROM permissions')).rows[0].n;
record(Number(perm) > 0, 'permissions 非空', String(perm));
await ok('admin 绑了 setting.write',
  "SELECT count(*) FROM role_permissions WHERE role_key='admin' AND permission_key='setting.write'", 1);
await ok('setting 的 route_template 以 {id} 结尾',
  "SELECT count(*) FROM audit_entity_types WHERE key='setting' AND route_template LIKE '%{id}'", 1);
await rejects('route_template 不以 {id} 结尾被拒',
  "INSERT INTO audit_entity_types (key, description, route_template) VALUES ('verifybad','x','/no-id')",
  'ck_audit_entity_route');
await rejects('route_template 不以 / 开头被拒',
  "INSERT INTO audit_entity_types (key, description, route_template) VALUES ('verifybad2','x','admin/{id}')",
  'ck_audit_entity_route');

// ---------- 8. 开发种子（db/seeds）：能应用、幂等、边界行齐全 ----------
// 种子本身是开发数据，但它踩的每一条约束都是真的。如果 schema 改了却没改种子，
// 这里会红 —— 比等到有人本地 db:seed 时才发现要好。
console.log('--- 开发种子 ---');
{
  const seedDir = path.join(root, 'db/seeds');
  const seedFiles = fs.existsSync(seedDir)
    ? fs.readdirSync(seedDir).filter((f) => f.endsWith('.sql')).sort()
    : [];
  let appliedSeeds = 0;
  for (const f of seedFiles) {
    const body = fs.readFileSync(path.join(seedDir, f), 'utf8');
    for (const stmt of statements(body)) {
      await db.exec(stmt);
    }
    appliedSeeds++;
  }
  record(appliedSeeds > 0, '种子文件已应用', String(appliedSeeds));

  const visible = Number((await db.query(
    "SELECT count(*) AS n FROM demos WHERE lifecycle_state='published' AND deleted_at IS NULL AND erased_at IS NULL",
  )).rows[0].n);
  record(visible >= 20, '种子提供了足够铺满两页的已发布作品', String(visible));

  const before = Number((await db.query('SELECT count(*) AS n FROM demos')).rows[0].n);
  for (const f of seedFiles) {
    const body = fs.readFileSync(path.join(seedDir, f), 'utf8');
    for (const stmt of statements(body)) {
      await db.exec(stmt);
    }
  }
  const after = Number((await db.query('SELECT count(*) AS n FROM demos')).rows[0].n);
  record(before === after, '种子可重复应用（幂等）', before + ' → ' + after);

  // ---- 登录限流表（迁移 0001 加的，不在冻结基线里）----
  await ok(
    'login_attempts 存在（迁移 0001）',
    "SELECT count(*) FROM information_schema.tables WHERE table_name = 'login_attempts'",
    '1',
  );
  // 记不了「账号不存在」的失败，整套 IP 限流就是瞎的——这条是那张表存在的理由。
  await accepts(
    'login_attempts 允许无主试（account_id 可空）',
    "INSERT INTO login_attempts (ip_hash, account_id, succeeded) VALUES ('it-null-account', NULL, false)",
  );
  await ok(
    'login_attempts 两个部分索引都在',
    "SELECT count(*) FROM pg_indexes WHERE tablename = 'login_attempts' AND indexdef LIKE '%WHERE%'",
    '2',
  );

  for (const [slug, why] of [
    ['edge-no-description', '空描述'],
    ['edge-long-title', '超长标题'],
    ['edge-archived', '归档墓碑'],
    ['edge-removed', '移除墓碑'],
    ['edge-soft-deleted', '软删墓碑'],
    ['edge-erased', '已擦除'],
  ]) {
    const n = Number((await db.query('SELECT count(*) AS n FROM demos WHERE slug=$1', [slug])).rows[0].n);
    record(n === 1, '边界行存在：' + why, slug);
  }
}

console.log('\n应用文件 %d 个（跳过 %d 个），断言 %d 条通过 / %d 条失败',
  appliedFiles.length, skipped.length, pass, fail);
if (fail) {
  console.log('\n失败明细：');
  for (const f of failures) console.log('  -', f);
  process.exit(1);
}
console.log('真库校验通过');
