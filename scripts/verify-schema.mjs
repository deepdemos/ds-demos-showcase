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

// ---------- 3b. users 投影表（迁移 0007：注册建行、业务列指向它）----------
console.log('--- users 投影 ---');
await accepts('建 users 投影行', "INSERT INTO users (sub) VALUES ('VERIFY-SUB-1')");
await ok('投影行可由 sub 对齐 accounts',
  "SELECT count(*) FROM users u JOIN accounts a ON a.sub = u.sub WHERE u.sub='VERIFY-SUB-1'", 1);
await rejects('sub 重复被拒', "INSERT INTO users (sub) VALUES ('VERIFY-SUB-1')", 'users_sub_key');
await ok('users 只剩四列（history_enabled 已删除，偏好归设置域）',
  "SELECT count(*) FROM pg_attribute WHERE attrelid='users'::regclass AND attnum>0 AND NOT attisdropped", 4);

// ---------- 4. 用户偏好：本人读写 + 级联 ----------
console.log('--- 用户偏好 ---');
await accepts('写用户偏好',
  "INSERT INTO user_preferences (account_id, key, value_type, value) VALUES (" + aid + ",'history.record','boolean','false'::jsonb)");
await ok('读回用户偏好',
  "SELECT value::text FROM user_preferences WHERE key='history.record'", 'false');
await db.exec("DELETE FROM users WHERE sub = 'VERIFY-SUB-1'");
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

// ---------- 5b. demo 域 16 条外键（迁移 0008）----------
// 这 16 列在此前全都没有外键：能插进不存在的 id，删父行会留下孤儿。
// 断言分两半 —— 「约束在不在」查 pg_constraint，「行为对不对」真插真删。
console.log('--- demo 域外键 ---');
const EXPECTED_FK = [
  ['demo_channels', 'fk_demo_channels_demo_id78', 'c'],
  ['demo_stats', 'fk_demo_stats_demo_id79', 'c'],
  ['demo_ratings', 'fk_demo_ratings_demo_id80', 'c'],
  ['demo_timeline', 'fk_demo_timeline_demo_id81', 'c'],
  ['demo_slug_history', 'fk_demo_slug_history_demo_id82', 'c'],
  ['demo_grants', 'fk_demo_grants_demo_id83', 'c'],
  ['demo_revision_files', 'fk_demo_revision_files_revision_id84', 'c'],
  ['demos', 'fk_demos_author_id85', 'n'],
  ['demo_revisions', 'fk_demo_revisions_created_by86', 'n'],
  ['demo_channels', 'fk_demo_channels_curated_by87', 'n'],
  ['demo_ratings', 'fk_demo_ratings_user_id88', 'n'],
  ['demo_timeline', 'fk_demo_timeline_actor_id89', 'n'],
  ['demo_grants', 'fk_demo_grants_granted_by90', 'n'],
  ['demo_revisions', 'fk_demo_revisions_prompt_id91', 'a'],
  ['demo_revision_files', 'fk_demo_revision_files_blob_hash92', 'a'],
  ['demo_grants', 'fk_demo_grants_capability93', 'a'],
];
for (const [table, name, deltype] of EXPECTED_FK) {
  await ok('外键 ' + name,
    "SELECT confdeltype FROM pg_constraint WHERE conname='" + name + "' AND conrelid='" + table + "'::regclass",
    deltype);
}

// 人列必须能指到 users.id；指向不存在的用户要被拒。
await db.exec("INSERT INTO users (sub) VALUES ('VERIFY-SUB-1')");
const uid = (await db.query("SELECT id FROM users WHERE sub='VERIFY-SUB-1'")).rows[0].id;
await accepts('demos.author_id 指向 users.id',
  "UPDATE demos SET author_id=" + uid + " WHERE id=" + did);
await rejects('demos.author_id 指向不存在的用户被拒',
  "UPDATE demos SET author_id=999999 WHERE id=" + did, 'fk_demos_author_id85');

// 共享实体引用：能力下线要先清授权；blob 不存在时引用要拒。
await rejects('demo_grants.capability 写未登记能力被拒',
  "INSERT INTO demo_grants (demo_id, capability) VALUES (" + did + ",'not.a.capability')",
  'fk_demo_grants_capability93');
await accepts('demo_grants.capability 写已登记能力通过',
  "INSERT INTO demo_grants (demo_id, capability) VALUES (" + did + ",'storage.read')");
await rejects('demo_revision_files.blob_hash 指向不存在的 blob 被拒',
  "INSERT INTO demo_revision_files (revision_id, path, blob_hash, size, mime) VALUES ((SELECT id FROM demo_revisions WHERE demo_id=" + did + " AND rev_no=2),'a.js',repeat('f',64),1,'text/javascript')",
  'fk_demo_revision_files_blob_hash92');

// 级联：删版本要把清单行一起带走（此前不会，孤儿会留下）。
await db.exec("INSERT INTO blobs (sha256, size, mime, storage_key, backend) VALUES (repeat('e',64),10,'text/html','verify/index.html','local')");
await accepts('清单行可写（blob 存在）',
  "INSERT INTO demo_revision_files (revision_id, path, blob_hash, size, mime) VALUES ((SELECT id FROM demo_revisions WHERE demo_id=" + did + " AND rev_no=2),'index.html',repeat('e',64),10,'text/html')");
await ok('清单行已写入',
  "SELECT count(*) FROM demo_revision_files f JOIN demo_revisions r ON r.id=f.revision_id WHERE r.demo_id=" + did, 1);
await db.exec("DELETE FROM demo_revisions WHERE demo_id=" + did + " AND rev_no=2");
await ok('删版本级联删清单行',
  "SELECT count(*) FROM demo_revision_files", 0);

// ---------- 5c. demo 域三条不变量触发器（迁移 0009）----------
// #1 revision 不可变、#2 指针必须是自己的版本、#10 上架渠道要求已审核。
// 这三条出错时全是静默的 —— 不报错，只是数据从此不对。
console.log('--- demo 域不变量 ---');
await ok('三条触发器都在',
  "SELECT count(*) FROM pg_trigger WHERE tgname IN ('trg_demo_revisions_immutable','trg_demos_guard','trg_demo_channels_listed_requires_approved') AND NOT tgisinternal",
  3);
await db.exec("INSERT INTO demo_revisions (demo_id, rev_no, demo_type, title) VALUES (" + did + ",3,'zip','inv')");
await rejects('不变量 1：改 revision 的内容列被拒',
  "UPDATE demo_revisions SET title='changed' WHERE demo_id=" + did + " AND rev_no=3",
  '一旦创建即不可变');
await accepts('不变量 1：改 deleted_at 允许',
  "UPDATE demo_revisions SET deleted_at=now() WHERE demo_id=" + did + " AND rev_no=3");
// 造第二个作品：它还是 pending，正好用来验 #2 的「跨作品」与 #10 的「未审核」。
await db.exec("INSERT INTO demos (slug, title, moderation_state, lifecycle_state) VALUES ('verify-demo-2','二号','pending','draft')");
const did2 = (await db.query("SELECT id FROM demos WHERE slug='verify-demo-2'")).rows[0].id;
await db.exec("INSERT INTO demo_revisions (demo_id, rev_no, demo_type, title) VALUES (" + did2 + ",1,'web','b1')");

await rejects('不变量 2：指针指向别的作品的版本被拒',
  "UPDATE demos SET current_revision_id=(SELECT id FROM demo_revisions WHERE demo_id=" + did2 + " AND rev_no=1) WHERE id=" + did,
  '不属于作品');
await accepts('不变量 2：指针指向自己的版本允许',
  "UPDATE demos SET current_revision_id=(SELECT id FROM demo_revisions WHERE demo_id=" + did + " AND rev_no=3) WHERE id=" + did);
await rejects('不变量 10：未审核作品上架渠道被拒',
  "INSERT INTO demo_channels (demo_id, channel, visibility) VALUES (" + did2 + ",'deep','listed')",
  '不能上架渠道');
await accepts('不变量 10：非 listed 渠道不受限',
  "INSERT INTO demo_channels (demo_id, channel, visibility) VALUES (" + did2 + ",'deep','unlisted')");
await db.exec("DELETE FROM demo_channels WHERE demo_id=" + did2 + " AND channel='deep'");
// 迁移 0014 之后，「能上架」的判据从「审核态 approved」变成「有对外版本」。
// 这两条原来钉的是审核态，必须跟着改口径——否则它们会替旧语义站岗，
// 让「作者改稿后线上作品消失」重新变成合法状态。
const rev2 = "(SELECT id FROM demo_revisions WHERE demo_id=" + did2 + " AND rev_no=1)";
await accepts('不变量 10：有对外版本后可以上架',
  "UPDATE demos SET moderation_state='approved', current_revision_id=" + rev2 + " WHERE id=" + did2 + ";" +
  " UPDATE demos SET published_revision_id=" + rev2 + " WHERE id=" + did2 + ";" +
  " INSERT INTO demo_channels (demo_id, channel, visibility) VALUES (" + did2 + ",'deep','listed');");
// 这条拦的是「把对外版本清空」——错误文案来自 trg_demos_guard（还有一个上架渠道），
// 不是渠道守卫那条「还没有对外版本」。两者都会拒，但拒绝的时机不同，
// 断言盯错文案就会以为门禁没生效。
await rejects('不变量 10 反向：上架中不能清掉对外版本',
  "UPDATE demos SET published_revision_id=NULL WHERE id=" + did2,
  '不能把对外版本清空');

// 擦除那条分支过去是无条件放行任何列，等于给「擦除」留了一扇任意改写的后门。
// 现在只允许清空内容列，身份列（demo_id / rev_no / created_at / source …）必须不动。
await rejects('不变量 1：擦除时改 demo_id 被拒',
  "UPDATE demo_revisions SET erased_at=now(), deleted_at=now(), demo_id=" + did2 + ", title=NULL, description=NULL, cover_url=NULL, cover_inline=NULL, entry_path=NULL, manifest_hash=NULL, file_count=NULL, total_bytes=NULL, prompt=NULL, prompt_id=NULL, video_url=NULL, model_hint=NULL WHERE demo_id=" + did + " AND rev_no=3",
  '不允许修改身份列');
await rejects('不变量 1：擦除时改 created_at 被拒',
  "UPDATE demo_revisions SET erased_at=now(), deleted_at=now(), created_at='2000-01-01', title=NULL, description=NULL, cover_url=NULL, cover_inline=NULL, entry_path=NULL, manifest_hash=NULL, file_count=NULL, total_bytes=NULL, prompt=NULL, prompt_id=NULL, video_url=NULL, model_hint=NULL WHERE demo_id=" + did + " AND rev_no=3",
  '不允许修改身份列');

// ---------- 5d. 渠道主数据（迁移 0010）----------
// 渠道是数据不是枚举（标准对象 §EN-1）—— 但「数据」不等于「随便写」。
console.log('--- 渠道主数据 ---');
await ok('channels 码值',
  "SELECT string_agg(code, ',' ORDER BY sort) FROM channels WHERE status='active'", 'deep,astra');
await rejects('未登记的渠道被拒',
  "INSERT INTO demo_channels (demo_id, channel, visibility) VALUES (" + did + ",'astr','hidden')",
  'fk_demo_channels_channel94');
await accepts('登记过的渠道通过',
  "INSERT INTO demo_channels (demo_id, channel, visibility) VALUES (" + did2 + ",'astra','hidden')");
await ok('渠道码格式受约束',
  "SELECT count(*) FROM pg_constraint WHERE conname='ck_channels_code'", 1);

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
  // ---- 登录尝试的标识符列（迁移 0003）----
  await ok(
    'login_attempts 有 identifier_hash（第②层按标识符计数）',
    "SELECT count(*) FROM information_schema.columns " +
      "WHERE table_name = 'login_attempts' AND column_name = 'identifier_hash'",
    '1',
  );
  await accepts(
    'login_attempts 的 identifier_hash 可空（迁移前的历史行不参与第②层）',
    "INSERT INTO login_attempts (ip_hash, succeeded) VALUES ('it-ip-null', false)",
  );
  await ok(
    'login_attempts 的标识符索引存在',
    "SELECT count(*) FROM pg_indexes WHERE indexname = 'ix_login_attempts_identifier'",
    '1',
  );

  // ---- 密码重置（迁移 0004）----
  await ok(
    'credentials 有 rotated_at（换密码后作废更早的会话）',
    "SELECT count(*) FROM information_schema.columns " +
      "WHERE table_name = 'credentials' AND column_name = 'rotated_at'",
    '1',
  );
  await accepts(
    'rotated_at 可空（既有凭证视作从未轮换）',
    "UPDATE credentials SET rotated_at = NULL WHERE kind = 'password'",
  );

  // ---- 通用验证码表（迁移 0002）----
  await ok(
    'verification_codes 存在（迁移 0002）',
    "SELECT count(*) FROM information_schema.tables WHERE table_name = 'verification_codes'",
    '1',
  );
  await accepts(
    'verification_codes 允许无账号的验证码（account_id 可空）',
    "INSERT INTO verification_codes (purpose, identifier_hash, code_hash, expires_at) " +
      "VALUES ('it-purpose', 'it-key', 'it-hash', now() + interval '5 minutes')",
  );
  // 同一串码在不同 purpose 下必须是两条独立记录（跨场景不能重放的前提）。
  await accepts(
    'verification_codes 同一标识符可有多用途记录',
    "INSERT INTO verification_codes (purpose, identifier_hash, code_hash, expires_at) " +
      "VALUES ('it-purpose-2', 'it-key', 'it-hash', now() + interval '5 minutes')",
  );
  await ok(
    'verification_codes 有效码的查找索引存在',
    "SELECT count(*) FROM pg_indexes WHERE indexname = 'ix_verification_codes_lookup'",
    '1',
  );

  await ok(
    'login_attempts 四个部分索引都在',
    "SELECT count(*) FROM pg_indexes WHERE tablename = 'login_attempts' AND indexdef LIKE '%WHERE%'",
    '4',
  );

  await ok(
    'login_attempts 有 kind（迁移 0005：发码与登录失败分开计数）',
    "SELECT count(*) FROM information_schema.columns WHERE table_name = 'login_attempts' AND column_name = 'kind'",
    '1',
  );

  await rejects(
    'login_attempts 的 kind 只接受 login / email_code',
    "INSERT INTO login_attempts (ip_hash, succeeded, kind) VALUES ('it-bad-kind', false, 'other')",
    'ck_login_attempts_kind',
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
