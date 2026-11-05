/**
 * 契约描述里的端点数目必须与真契约一致。
 *
 * # 为什么需要这个
 *
 * `api/openapi.yaml` 的 `info.description` 里写着「共 N 个端点」。这句话漂过两次：
 * 先是长期停在「三个端点」（那时已经十几个），后来写成 18（实际 24）。
 * 两次都是靠独立 review 读出来的，门禁一声不响——因为**没有任何东西在读这段散文**。
 *
 * 这是这个项目里最反复出现的一类错：**文字里的数字没有判据**。
 * 病情轻（不影响行为），但它会误导每一个拿这份契约当事实源的人。
 *
 * # 判据
 *
 * 描述里出现的「共 N 个端点」必须等于 `api/surface.json` 里的端点数目——
 * 后者由 `scripts/check-api-compat.mjs` 从生成的 Go 代码里重新签出来，是机器产物；
 * 拿机器产物去核对散文，是这件事唯一稳的核法（不靠人眼数 yaml）。
 *
 * # 它不能证明什么
 *
 * 它只核对**这个数字**。描述里对端点分组的文字说明（「身份」「生命周期」）是否
 * 跟实现一一对应，仍然只能靠 review；门禁只保证「数得清的那部分数得对」。
 */
import { existsSync, readFileSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const ROOT = resolve(dirname(fileURLToPath(import.meta.url)), '..');
export const SPEC = join(ROOT, 'api', 'openapi.yaml');
export const SURFACE = join(ROOT, 'api', 'surface.json');

const STATED = /共\s*(\d+)\s*个端点/;

/**
 * 校验 `info.description` 里声明的端点数目。
 *
 * @param {{spec: string, surface: object}} input
 * @returns {{ok: boolean, problems: string[], stated: number|null, actual: number}}
 */
export function checkApiDoc({ spec, surface }) {
  const problems = [];
  const text = String(spec ?? '');
  const actual = Object.keys(surface?.endpoints ?? {}).length;

  const m = STATED.exec(text);
  if (!m) {
    problems.push('契约描述里没有「共 N 个端点」这一句：没有它，读者无法判断这份契约覆盖了多少接口。');
    return { ok: false, problems, stated: null, actual };
  }
  const stated = Number(m[1]);
  if (stated !== actual) {
    problems.push(
      `契约描述写「共 ${stated} 个端点」，实际 ${actual} 个：` +
        '改端点时顺手改这句话，否则拿这份契约当事实源的人会被误导。',
    );
  }
  return { ok: problems.length === 0, problems, stated, actual };
}

function main() {
  for (const [label, path] of [
    ['契约', SPEC],
    ['端点基线', SURFACE],
  ]) {
    if (!existsSync(path)) {
      console.error(`契约描述门禁失败：找不到${label} ${path}（先跑 node scripts/check-api-compat.mjs --update）`);
      process.exit(1);
    }
  }
  const result = checkApiDoc({
    spec: readFileSync(SPEC, 'utf8'),
    surface: JSON.parse(readFileSync(SURFACE, 'utf8')),
  });
  if (!result.ok) {
    console.error('契约描述门禁失败：');
    for (const p of result.problems) console.error(`  - ${p}`);
    process.exit(1);
  }
  console.log(`契约描述门禁通过（描述与契约都是 ${result.actual} 个端点）`);
}

if (process.argv[1] && resolve(process.argv[1]) === resolve(fileURLToPath(import.meta.url))) {
  main();
}
