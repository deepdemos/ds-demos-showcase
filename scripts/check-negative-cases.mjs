/**
 * 反例矩阵：每个端点至少钉一条「它必须拒绝什么」。
 *
 * # 为什么需要这个
 *
 * 2026-11-04 以前，`POST /api/v1/demos` 一条反例测试都没有——鉴权坏了也全绿。
 * 同类问题在这个仓库里反复出现（`hidden` 渠道不进详情、`unapproved` 的发布能过、
 * 外包名伪造、`decision:"yes"` 落成驳回），共同点都是「**正向路径测得很足，反向没人写**」。
 * 正向用例证明「该给的给了」，但证明不了「不该给的没给」——而后者才是安全缺陷的形状。
 *
 * # 判据
 *
 * - 契约里每个端点（`api/openapi.yaml` 的真实路径，经 `scripts/check-api-compat.mjs`
 *   重签进 `api/surface.json`）必须至少一条反例；
 * - 契约外的处理器（`/preview`、`/download`）也要，因为它们同样对外可达；
 * - 每条反例必须指向**真实存在的测试**：文件存在、且文件里真的有那个测试函数/用例名。
 *   「写了说明但没写测试」在本门禁里等同于没写。
 *
 * # 它不能证明什么
 *
 * 它证明「有一条测试叫这个名字」，不能证明这条测试**断言得对**。
 * 断言质量靠反向验证（把实现改坏一次，看它是否真的红）——那件事没人能替。
 */
import { existsSync, readFileSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const ROOT = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const CASES_FILE = join(ROOT, 'api', 'negative-cases.json');
const SURFACE_FILE = join(ROOT, 'api', 'surface.json');

function readJSON(path) {
  return JSON.parse(readFileSync(path, 'utf8'));
}

/** Go 测试的写法是 `func TestXxx(t *testing.T) {`。 */
function goTestName(name) {
  return new RegExp(`^func\\s+${escapeRe(name)}\\s*\\(`, 'm');
}

/** vitest 的写法是 `test('名字', …)` 或 `it('名字', …)`。 */
function tsTestName(name) {
  return new RegExp(`(?:^|\\s)(?:test|it)\\s*\\(\\s*['"\`]${escapeRe(name)}['"\`]`, 'm');
}

function escapeRe(s) {
  return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}

/** 按扩展名挑断言方式：`.go` 认函数名，`.ts/.tsx` 认用例名。 */
function findTest(file, name, root = ROOT) {
  const abs = join(root, file);
  if (!existsSync(abs)) return `测试文件不存在：${file}`;
  const source = readFileSync(abs, 'utf8');
  const isGo = file.endsWith('.go');
  const pattern = isGo ? goTestName(name) : tsTestName(name);
  if (!pattern.test(source)) {
    return isGo
      ? `${file} 里没有测试函数 ${name}`
      : `${file} 里没有名为「${name}」的用例`;
  }
  return null;
}

/**
 * 校验矩阵。
 *
 * @returns {{problems: string[], stats: {endpoints: number, cases: number}}}
 */
export function checkNegativeCases({ casesFile = CASES_FILE, surfaceFile = SURFACE_FILE, root = ROOT, requiredOutside } = {}) {
  const problems = [];
  if (!existsSync(casesFile)) {
    return { problems: ['缺少 api/negative-cases.json'], stats: { endpoints: 0, cases: 0 } };
  }
  const matrix = readJSON(casesFile);
  const surface = readJSON(surfaceFile);

  const declared = matrix.endpoints ?? {};
  const outside = matrix.outsideContract ?? {};

  // 1) 契约里的每个端点都必须有反例。
  const contractEndpoints = Object.keys(surface.endpoints ?? {});
  for (const endpoint of contractEndpoints) {
    if (!Array.isArray(declared[endpoint]) || declared[endpoint].length === 0) {
      problems.push(`契约端点没有反例：${endpoint}`);
    }
  }

  // 2) 矩阵里不能有契约里已不存在的端点（删端点时忘了收账）。
  for (const endpoint of Object.keys(declared)) {
    if (!contractEndpoints.includes(endpoint)) {
      problems.push(`矩阵登记了契约里不存在的端点：${endpoint}`);
    }
  }

  // 3) 契约外处理器也必须覆盖（它们同样对外可达）。
  const outsideRequired = requiredOutside ?? ['GET /preview/{slug}/*', 'GET /download/{slug}'];
  for (const endpoint of outsideRequired) {
    if (!Array.isArray(outside[endpoint]) || outside[endpoint].length === 0) {
      problems.push(`契约外端点没有反例：${endpoint}`);
    }
  }

  // 4) 每条反例都得指向真实测试，且说明不能空着。
  let cases = 0;
  for (const [endpoint, list] of [...Object.entries(declared), ...Object.entries(outside)]) {
    for (const entry of list) {
      cases += 1;
      if (!entry.case || entry.case.trim().length < 8) {
        problems.push(`${endpoint}：反例说明太短或为空`);
      }
      if (!entry.test || !entry.test.file || !entry.test.name) {
        problems.push(`${endpoint}：反例「${entry.case ?? '?'}」没写测试文件与测试名`);
        continue;
      }
      const missing = findTest(entry.test.file, entry.test.name, root);
      if (missing) problems.push(`${endpoint}：${missing}`);
    }
  }

  return {
    problems,
    stats: { endpoints: contractEndpoints.length + outsideRequired.length, cases },
  };
}

const isMain = process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url);
if (isMain) {
  const { problems, stats } = checkNegativeCases();
  if (problems.length > 0) {
    console.error(`[error] 反例矩阵不合格（${problems.length} 条）：`);
    for (const p of problems) console.error(`  - ${p}`);
    console.error('\n反例矩阵的规则见 api/negative-cases.json 顶部说明与 scripts/check-negative-cases.mjs。');
    process.exit(1);
  }
  console.log(`反例矩阵通过（${stats.endpoints} 个端点 / ${stats.cases} 条反例，全部指向真实测试）`);
}
