#!/usr/bin/env node
// 分层门禁：backend/ 里的包只能往「允许的方向」import。
//
// 这不是风格洁癖。下面每条规则背后都有一个具体的坏结果，见 docs/后端分层约定.md。
// 门禁只看 import 路径，**不判断这个包是不是真的只做这一层的事**——它抓不到
// 「httpapi 里写了业务规则」这种。那要靠 review。
import fs from 'node:fs';
import path from 'node:path';
import { pathToFileURL } from 'node:url';

const MOD = 'github.com/deepdemos/ds-demos-showcase/backend/';

/** 从文件路径判断它属于哪一层。返回 null 表示不在受管目录里。 */
export function layerOf(file) {
  const m = file.replace(/\\/g, '/').match(/(?:^|\/)backend\/(.+)$/);
  if (!m) return null;
  const rest = m[1];
  if (rest.startsWith('cmd/')) return 'cmd';
  const inner = rest.match(/^internal\/([^/]+)(?:\/([^/]+))?/);
  if (!inner) return null;
  const [, top, sub] = inner;
  if (top === 'domain') return sub ? `domain:${sub}` : 'domain';
  if (top === 'kernel') return sub ? `kernel:${sub}` : 'kernel';
  return top; // platform / httpapi / api / store
}

/** 从源码里抽本模块内部 import。 */
export function internalImports(source) {
  const out = [];
  for (const block of source.matchAll(/^import \(\n([\s\S]*?)^\)$/gm)) {
    for (const line of block[1].split('\n')) {
      const m = line.match(/"([^"]+)"/);
      if (m && m[1].startsWith(MOD)) out.push(m[1].slice(MOD.length));
    }
  }
  for (const m of source.matchAll(/^import "([^"]+)"$/gm)) {
    if (m[1].startsWith(MOD)) out.push(m[1].slice(MOD.length));
  }
  return out;
}

/** 层 → 它能 import 的层的判据。返回 null 表示允许，返回字符串表示原因。 */
export function violation(from, to) {
  const base = (l) => l.split(':')[0];

  // 装配层：cmd 可以拿任何东西。
  if (from === 'cmd') return null;

  // 生成物：只依赖公共库。它们被谁 import 由别人负责。
  if (from === 'api' || from === 'store') {
    if (to === from) return null; // 同包不同文件，不产生 import
    if (['api', 'store', 'httpapi', 'platform', 'kernel'].includes(base(to)) || base(to) === 'domain') {
      return `生成物不该 import 本仓库其它包（${to}）——重新生成会把它覆盖掉`;
    }
    return null;
  }

  // 基础设施：不知道业务的存在，也不知道 kernel。
  if (from === 'platform') {
    if (['api', 'store', 'httpapi', 'kernel'].includes(base(to)) || base(to) === 'domain') {
      return `platform 不得依赖 ${to}：基础设施一旦知道业务，业务就再也换不掉它`;
    }
    return null;
  }

  // kernel：可以有机制，但不得知道任何具体业务域；子包之间也互相独立。
  if (base(from) === 'kernel') {
    if (base(to) === 'domain') {
      return `kernel 不得依赖 ${to}：kernel 是所有域共用的机制，认识某个域就再也共用不了`;
    }
    if (['api', 'httpapi'].includes(base(to))) {
      return `kernel 不得依赖 ${to}：机制不该知道 HTTP 或契约长什么样`;
    }
    if (base(to) === 'kernel' && to !== from) {
      return `${from} 不得 import ${to}：kernel 子包各自独立，串起来就会变成第二个 domain`;
    }
    return null;
  }

  // 业务域：不碰传输层，也不互相 import。
  if (base(from) === 'domain') {
    if (['api', 'httpapi'].includes(base(to))) {
      return `domain 不得依赖 ${to}：业务逻辑不该知道 HTTP 长什么样`;
    }
    if (base(to) === 'domain' && to !== from) {
      return `${from} 不得 import ${to}：域之间只能通过 platform 或已发布接口协作，否则两个域就粘成一个了`;
    }
    return null;
  }

  // 传输层：可以拿业务、生成物、基础设施。
  if (from === 'httpapi') return null;

  return null;
}

export function inspectLayering(cwd = process.cwd()) {
  const root = path.join(cwd, 'backend');
  const violations = [];
  let scanned = 0;

  const walk = (dir) => {
    for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
      const full = path.join(dir, entry.name);
      if (entry.isDirectory()) {
        walk(full);
        continue;
      }
      if (!entry.name.endsWith('.go') || entry.name.endsWith('_test.go')) continue;
      const rel = path.relative(cwd, full).replace(/\\/g, '/');
      const from = layerOf(rel);
      if (!from) continue;
      scanned++;
      for (const dep of internalImports(fs.readFileSync(full, 'utf8'))) {
        const to = layerOf(`${dep}.go`);
        if (!to) continue;
        const why = violation(from, to);
        if (why) violations.push({ file: rel, dep, why });
      }
    }
  };

  walk(root);
  return { scanned, violations };
}

function main() {
  const { scanned, violations } = inspectLayering();
  if (violations.length > 0) {
    for (const v of violations) console.log(`::error file=${v.file}::${v.file} → ${v.dep}：${v.why}`);
    console.error(`分层门禁失败（${violations.length} 条）。约定见 docs/后端分层约定.md。`);
    process.exit(1);
  }
  console.log(`分层门禁通过（${scanned} 个 Go 文件，import 方向全部合法）`);
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  main();
}
