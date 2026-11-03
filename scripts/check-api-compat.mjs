#!/usr/bin/env node
// 契约兼容门禁：把 OpenAPI 生成的 Go 类型表面抽成快照，和已提交的基线比。
//
// 为什么不直接 diff YAML：YAML 里改一个缩进、调一个顺序都会产生 diff，而它们
// 对调用方毫无影响。真正决定「会不会打断调用方」的是**生成出来的类型表面**——
// JSON 字段名、可空性、类型、枚举取值、端点与参数。这一层稳定得多。
//
// 边界写在 api/COMPATIBILITY.md：它抓不到「同名改语义」和「加枚举值导致客户端
// switch 不穷尽」。别把「门禁绿了」当成「兼容性没问题」。
import fs from 'node:fs';
import path from 'node:path';
import { pathToFileURL } from 'node:url';

// 生成物按「类型」与「服务端」拆成两份（见 api/oapi-codegen-types.yaml）。
// 兼容门禁读的是 Go 类型表面，所以两份拼起来当一个源读。
export const GENERATED = [
  'backend/internal/api/types.gen.go',
  'backend/internal/api/server.gen.go',
];
export const BASELINE = 'api/surface.json';

/** 抽出「调用方能看见的东西」。只认 oapi-codegen 生成出来的那几种形状。 */
export function extractSurface(goSource) {
  const models = {};
  const enums = {};
  const endpoints = {};

  // ① 枚举常量：const ( Name Type = "value" )
  for (const block of goSource.matchAll(/^const \(\n([\s\S]*?)^\)$/gm)) {
    for (const line of block[1].split('\n')) {
      const m = line.match(/^\t(\w+)\s+(\w+)\s*=\s*"([^"]*)"$/);
      if (!m) continue;
      const [, , type, value] = m;
      (enums[type] ??= []).push(value);
    }
  }

  // ② struct：字段名 + Go 类型 + json 标签
  for (const block of goSource.matchAll(/^type (\w+) struct \{\n([\s\S]*?)^\}$/gm)) {
    const [, name, body] = block;
    const fields = {};
    for (const line of body.split('\n')) {
      const m = line.match(/^\t(\w+)\s+([^\s`]+)\s+`json:"([^",]+)[^"]*"`$/);
      if (!m) continue;
      const [, , goType, jsonName] = m;
      const optional = goType.startsWith('*');
      fields[jsonName] = { type: goType.replace(/^\*/, ''), optional };
    }
    if (Object.keys(fields).length > 0) models[name] = fields;
  }

  // ③ ServerInterface：方法 + 路由注释里的 (方法 路径) + 参数类型
  const iface = goSource.match(/^type ServerInterface interface \{\n([\s\S]*?)^\}$/m);
  if (iface) {
    const lines = iface[1].split('\n');
    let route = null;
    for (const line of lines) {
      const r = line.match(/^\t\/\/ \((\w+) (\/\S+)\)$/);
      if (r) {
        route = `${r[1]} ${r[2]}`;
        continue;
      }
      // 签名有三种形态：只有 (w, r)；带路径参数 (..., slug string)；带查询参数
      // (..., params ListDemosParams)。三种都要认，所以先取剩余参数再解析。
      const m = line.match(/^\t(\w+)\(w http\.ResponseWriter, r \*http\.Request(.*)\)$/);
      if (!m) continue;
      const [, method, rest] = m;
      const paramsType = rest.match(/params (\w+)/)?.[1] ?? null;
      const pathParams = rest
        .split(',')
        .map((part) => part.trim())
        .filter((part) => part && !part.startsWith('params ')).length;
      endpoints[route ?? method] = { method, pathParams, params: paramsType };
      route = null;
    }
  }

  for (const type of Object.keys(enums)) enums[type].sort();
  return { models, enums, endpoints };
}

/** 把两份快照比成一条条判定。breaking 会让门禁红。 */
export function diffSurface(base, cur) {
  const findings = [];
  const push = (level, scope, message) => findings.push({ level, scope, message });

  // 端点
  for (const route of Object.keys(base.endpoints)) {
    if (!cur.endpoints[route]) {
      push('breaking', route, '端点被移除');
      continue;
    }
    const a = base.endpoints[route];
    const b = cur.endpoints[route];
    if (a.params && !b.params) push('breaking', route, '查询参数被移除');
    if (!a.params && b.params) push('warn', route, '新增查询参数（对老客户端是可选，但服务端要能容忍不传）');
  }
  for (const route of Object.keys(cur.endpoints)) {
    if (!base.endpoints[route]) push('ok', route, '新增端点');
  }

  // 模型
  for (const [name, fields] of Object.entries(base.models)) {
    const now = cur.models[name];
    if (!now) {
      push('breaking', name, '模型被移除（或改名）');
      continue;
    }
    for (const [field, spec] of Object.entries(fields)) {
      const next = now[field];
      if (!next) {
        push('breaking', `${name}.${field}`, '字段被移除（或改名）');
        continue;
      }
      if (next.type !== spec.type) {
        push('breaking', `${name}.${field}`, `类型从 ${spec.type} 改成 ${next.type}`);
      } else if (spec.optional && !next.optional) {
        push('breaking', `${name}.${field}`, '从可空变成必填（老数据里 null 的会被判非法）');
      } else if (!spec.optional && next.optional) {
        push('warn', `${name}.${field}`, '从必填变成可空（客户端要处理 null）');
      }
    }
    for (const field of Object.keys(now)) {
      if (!fields[field]) push('warn', `${name}.${field}`, '新增字段（老客户端会忽略，但响应体积与文档要跟上）');
    }
  }
  for (const name of Object.keys(cur.models)) {
    if (!base.models[name]) push('ok', name, '新增模型');
  }

  // 枚举
  for (const [type, values] of Object.entries(base.enums)) {
    const now = cur.enums[type];
    if (!now) {
      push('breaking', type, '枚举类型被移除');
      continue;
    }
    for (const value of values) {
      if (!now.includes(value)) push('breaking', `${type}="${value}"`, '枚举取值被移除（正在用它的人会收到未知值）');
    }
    for (const value of now) {
      if (!values.includes(value)) {
        push('warn', `${type}="${value}"`, '新增枚举取值（客户端若 switch 穷尽，会走到 default）');
      }
    }
  }
  for (const type of Object.keys(cur.enums)) {
    if (!base.enums[type]) push('ok', type, '新增枚举');
  }

  return findings;
}

export function readSurface(files = GENERATED, cwd = process.cwd()) {
  const list = Array.isArray(files) ? files : [files];
  const body = list.map((f) => fs.readFileSync(path.join(cwd, f), 'utf8')).join('\n');
  return extractSurface(body);
}

function main() {
  const update = process.argv.includes('--update');
  const cwd = process.cwd();
  const cur = readSurface(GENERATED, cwd);

  if (update) {
    fs.writeFileSync(path.join(cwd, BASELINE), JSON.stringify(cur, null, 2) + '\n');
    console.log(`已更新 ${BASELINE}（${Object.keys(cur.models).length} 模型 / ${Object.keys(cur.enums).length} 枚举 / ${Object.keys(cur.endpoints).length} 端点）`);
    return;
  }

  const basePath = path.join(cwd, BASELINE);
  if (!fs.existsSync(basePath)) {
    console.error(`${BASELINE} 不存在。首次启用跑一次：node scripts/check-api-compat.mjs --update`);
    process.exit(1);
  }
  const base = JSON.parse(fs.readFileSync(basePath, 'utf8'));

  const findings = diffSurface(base, cur);
  const breaking = findings.filter((f) => f.level === 'breaking');
  const warns = findings.filter((f) => f.level === 'warn');

  for (const f of breaking) console.log(`::error::[breaking] ${f.scope}：${f.message}`);
  for (const f of warns) console.log(`[warn] ${f.scope}：${f.message}`);

  if (breaking.length > 0) {
    console.error(`契约兼容门禁失败（${breaking.length} 条破坏性变更）。`);
    console.error('要么把改动退回兼容形态（加字段不删字段、加枚举值不删值），要么升主版本并在 api/COMPATIBILITY.md 记一笔。');
    console.error('确认这是有意的破坏性变更后，用 --update 重签基线，并在 PR 描述里写明影响面。');
    process.exit(1);
  }
  console.log(`契约兼容门禁通过（${Object.keys(cur.models).length} 模型 / ${Object.keys(cur.enums).length} 枚举 / ${Object.keys(cur.endpoints).length} 端点，无破坏性变更${warns.length ? `，${warns.length} 条提示` : ''}）`);
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  main();
}
