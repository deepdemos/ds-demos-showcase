#!/usr/bin/env node
// 契约门禁：把「文档里写死的目录表 / 权限键 / 注册表引用」互相钉起来。
//
// 为什么需要：这些表是**跨文档引用**的——权限键在治理域 §4.3 定义、在端点表与标准对象里被引用；
// 审计实体类型在标准对象 §EN-6 定义、在生命周期 §11.4 的 registry.yaml 里按 `entity_type` 引用；
// GC 对象类型在 §EN-7 定义、在 registry.yaml 的 `derivatives[].kind` 里引用。
// 任何一侧加了一行、另一侧没跟上，文档都能照常渲染，只有写代码的人会发现——这类漂移
// 之前是人工抓的（19 vs 20 权限、`models` 这种不存在的表名），所以落成四条机器断言：
//
//   A 权限键 —— §4.3 表里每个权限都必须在 `admin` 列有绑定；键唯一且形如 `域.动作`；
//      标题里的「（N 个）」必须等于实际行数；正文里任何 `域.动作` 形式的引用都必须是已登记的键。
//   B 审计动作 —— §EN-5 种子唯一、小写下划线；正文声明的条数必须等于种子条数。
//   C 审计实体类型 —— §EN-6 种子唯一；正文声明的条数必须等于种子条数。
//   D GC 对象类型 —— §EN-7 种子唯一；生命周期 registry.yaml 里每个 `kind` / `entity_type`
//      都必须在目录表里；`gc_queue.reason` 的 CHECK 取值必须与 §EN-8 一致。
//
// 只做文档级校验：不解析 DDL 是否能在 PG 上执行（那是各域自己的验证脚本），也不比对后端常量
// （后端接上之后由 `permission-contract` / `audit-contract` job 扩展）。
import fs from 'node:fs';
import path from 'node:path';

const RETIRED_PREFIXES = ['tag', 'setting', 'announcement'];

function read(root, rel) {
  const full = path.join(root, rel);
  return fs.existsSync(full) ? fs.readFileSync(full, 'utf8') : null;
}

/** 解析治理域 §4.3 的权限表。 */
export function parsePermissions(text) {
  const start = text.indexOf('### 4.3 权限清单');
  if (start < 0) return { declared: null, keys: [] };
  const end = text.indexOf('\n### 4.4', start);
  const block = text.slice(start, end < 0 ? text.length : end);
  const declared = Number((block.match(/^### 4\.3 权限清单（(\d+) 个）/m) || [])[1] ?? NaN);
  const keys = [];
  for (const line of block.split('\n')) {
    if (!line.startsWith('|')) continue;
    const cells = line.split('|').map((c) => c.trim());
    const keyCell = cells[2] || '';
    const match = keyCell.match(/^`([a-z][a-z0-9_]*\.[a-z][a-z0-9_]*)`$/);
    if (!match) continue;
    const adminCell = cells[cells.length - 2] || '';
    keys.push({ key: match[1], admin: adminCell.includes('✅') });
  }
  return { declared, keys };
}

/** 正文里所有形如 `域.动作` 的引用（只认已知权限前缀，避免误伤表名）。 */
export function parsePermissionRefs(text, prefixes, verbs) {
  // 行内豁免：反例（「写成 `demo.erase` 会…」）、历史变更（「`tag.edit` → 改名」）与退役记录不是引用。
  const EXEMPT = /改名|退役|拿掉|写成|反例|不允许|已停用|移除|禁止|历史/;
  const refs = [];
  for (const line of text.split('\n')) {
    if (EXEMPT.test(line)) continue;
    if (/^\|\s*v\d/.test(line)) continue; // 变更记录行是历史，不是活引用
    for (const match of line.matchAll(/`([a-z][a-z0-9_]*)\.([a-z][a-z0-9_]*)`/g)) {
      if (!prefixes.has(match[1]) || !verbs.has(match[2])) continue;
      const after = line.slice(match.index + match[0].length, match.index + match[0].length + 12);
      if (after.includes('→')) continue; // `旧键` → `新键` 的改名写法
      refs.push(match[1] + '.' + match[2]);
    }
  }
  return refs;
}

/** 解析 `INSERT INTO <table> (...) VALUES (...), (...);` 里的首列。 */
export function parseSeeds(text, table) {
  const start = text.indexOf(`INSERT INTO ${table}`);
  if (start < 0) return null;
  const end = text.indexOf(';', start);
  const block = text.slice(start, end < 0 ? text.length : end);
  return [...block.matchAll(/\(\s*'([^']+)'/g)].map((m) => m[1]);
}

/** 正文里「**N 条**」这种声明（取离 `anchor` 最近的那一处）。 */
export function parseDeclaredCount(text, anchor, pattern = /\*\*(\d+)\s*条/) {
  const at = text.indexOf(anchor);
  if (at < 0) return null;
  const window = text.slice(at, at + 1200);
  const match = window.match(pattern);
  return match ? Number(match[1]) : null;
}

/** 从 registry.yaml 片段里抽 kind / entity_type（不引 YAML 依赖，正则够用且可解释）。 */
export function parseRegistryRefs(text) {
  const start = text.indexOf('```yaml');
  const end = start < 0 ? -1 : text.indexOf('```', start + 7);
  const block = start < 0 ? '' : text.slice(start, end);
  const kinds = [...block.matchAll(/kind:\s*([a-z][a-z0-9_]*)/g)].map((m) => m[1]);
  const entityTypes = [...block.matchAll(/entity_type:\s*([a-z][a-z0-9_]*)/g)].map((m) => m[1]);
  return { kinds: [...new Set(kinds)], entityTypes: [...new Set(entityTypes)] };
}

/** 从 `CHECK (reason IN ('a','b'))` 抽取值。 */
export function parseCheckValues(text, column) {
  const match = text.match(new RegExp(`CHECK\\s*\\(\\s*${column}\\s+IN\\s*\\(([^)]*)\\)`));
  if (!match) return null;
  return [...match[1].matchAll(/'([^']*)'/g)].map((m) => m[1]);
}

export function inspectContracts(docs, root = process.cwd()) {
  const problems = [];
  const push = (msg) => problems.push(msg);

  // A 权限键
  const { declared, keys } = parsePermissions(docs.governance);
  if (keys.length === 0) push('治理域: 没有解析到 §4.3 的权限表');
  const seen = new Set();
  for (const { key, admin } of keys) {
    if (seen.has(key)) push(`治理域 §4.3: 权限键重复 \`${key}\``);
    seen.add(key);
    if (!admin) push(`治理域 §4.3: \`${key}\` 没有绑到 admin（admin 绑不全 = 有人调它必定 403，而界面照常显示）`);
  }
  if (Number.isFinite(declared) && declared !== keys.length) {
    push(`治理域 §4.3: 标题写「${declared} 个」，实际 ${keys.length} 行`);
  }
  const prefixes = new Set([...keys.map((k) => k.key.split('.')[0]), ...RETIRED_PREFIXES]);
  const ACTIONS = ['read','write','edit','delete','status','review','run','curate','featured','rebuild','moderate','handle','merge','erase'];
  const verbs = new Set([...keys.map((k) => k.key.split('.')[1]), ...ACTIONS]);
  for (const [name, text] of Object.entries(docs)) {
    if (!text) continue;
    for (const ref of new Set(parsePermissionRefs(text, prefixes, verbs))) {
      if (!seen.has(ref)) push(`${name}: 引用了未登记的权限键 \`${ref}\`（§4.3 里没有；已退役的前缀 tag./setting./announcement. 也不允许）`);
    }
  }

  // 目录表的种子与 CHECK 取值域都在 db/schema 里（表结构搬迁后，文档只留「N 条」声明）
  const schema = docs.schema ?? [...(function* () {
    const dir = path.join(root, 'db', 'schema');
    if (!fs.existsSync(dir)) return;
    for (const f of fs.readdirSync(dir).sort()) {
      if (f.endsWith('.sql')) yield fs.readFileSync(path.join(dir, f), 'utf8');
    }
  })()].join('\n');

  // B 审计动作
  const actions = parseSeeds(schema, 'audit_actions');
  if (!actions) push('db/schema/20-governance.sql: 没有解析到 audit_actions 种子');
  else {
    const dup = actions.filter((k, i) => actions.indexOf(k) !== i);
    for (const key of new Set(dup)) push(`标准对象 §EN-5: 审计动作重复 \`${key}\``);
    for (const key of actions) {
      if (!/^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)?$/.test(key)) push(`标准对象 §EN-5: 审计动作 \`${key}\` 不合命名（小写 snake_case，必要时一段点分）`);
    }
    const declaredCount = parseDeclaredCount(docs.standard, '§EN-5');
    if (declaredCount !== null && declaredCount !== actions.length) {
      push(`标准对象 §EN-5: 正文写「${declaredCount} 条」，种子实际 ${actions.length} 条`);
    }
  }

  // C 审计实体类型
  const entities = parseSeeds(schema, 'audit_entity_types');
  if (!entities) push('db/schema/20-governance.sql: 没有解析到 audit_entity_types 种子');
  else {
    const dup = entities.filter((k, i) => entities.indexOf(k) !== i);
    for (const key of new Set(dup)) push(`标准对象 §EN-6: 审计实体类型重复 \`${key}\``);
    const declaredCount = parseDeclaredCount(docs.standard, '§EN-6');
    if (declaredCount !== null && declaredCount !== entities.length) {
      push(`标准对象 §EN-6: 正文写「${declaredCount} 条」，种子实际 ${entities.length} 条`);
    }
  }

  // D GC 对象类型 + 注册表引用
  const gcKinds = parseSeeds(schema, 'gc_kinds');
  if (!gcKinds) push('标准对象 §EN-7: 没有解析到 gc_kinds 种子（别再写成散文了，散文没法被机器校验）');
  const registry = parseRegistryRefs(docs.lifecycle);
  if (gcKinds) {
    for (const kind of registry.kinds) {
      if (!gcKinds.includes(kind)) push(`生命周期 registry.yaml: derivatives 里的 kind \`${kind}\` 不在 §EN-7 的 gc_kinds 目录表里`);
    }
  }
  if (entities) {
    for (const type of registry.entityTypes) {
      if (!entities.includes(type)) push(`生命周期 registry.yaml: entity_type \`${type}\` 不在 §EN-6 的 audit_entity_types 里`);
    }
  }
  // db/schema 是事实源；拿不到时（单测 fixture / 迁移期文档）退回文档里的 DDL
  const checkValues = parseCheckValues(schema, 'reason') ?? parseCheckValues(docs.governance ?? '', 'reason');
  const declaredReasons = docs.standard.match(/^取值域\s*[：:]\s*(.+)$/m);
  if (checkValues && declaredReasons) {
    const fromDoc = [...declaredReasons[1].matchAll(/`([a-z_]+)`/g)].map((m) => m[1]).sort();
    const fromDdl = [...checkValues].sort();
    if (fromDoc.join(',') !== fromDdl.join(',')) {
      push(`gc_queue.reason: DDL 的 CHECK 是 [${fromDdl.join(', ')}]，§EN-8 的取值域是 [${fromDoc.join(', ')}]`);
    }
  }

  return problems;
}

export function run(root = process.cwd()) {
  const docs = {
    governance: read(root, 'docs/治理域设计.md'),
    standard: read(root, 'docs/标准对象/20-枚举值域.md'),
    lifecycle: read(root, 'docs/数据生命周期设计.md'),
    identity: read(root, 'docs/身份服务模型设计.md'),
    demo: read(root, 'docs/Demo域设计.md'),
    entity: read(root, 'docs/实体域数据结构设计.md'),
    tag: read(root, 'docs/标签域数据结构设计.md'),
    ddl: read(root, 'docs/DDL约定.md'),
  };
  for (const [name, text] of Object.entries(docs)) {
    if (!text) delete docs[name];
  }
  return { problems: inspectContracts(docs, root), docs: Object.keys(docs) };
}

if (import.meta.url === `file://${process.argv[1]}` || process.argv[1]?.endsWith('check-contracts.mjs')) {
  const { problems } = run();
  if (problems.length > 0) {
    console.error('契约门禁失败：');
    for (const problem of problems) console.error(`  - ${problem}`);
    console.error('\n修法：目录表是唯一事实源——先改标准对象/治理域的种子，再让引用跟着改；反过来（先改引用）一定漏。');
    process.exit(1);
  }
  console.log('契约门禁通过（权限键 / 审计动作 / 审计实体类型 / GC 对象类型 + registry 引用一致）');
}
