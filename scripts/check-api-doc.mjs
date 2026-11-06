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
 * 它核对两件事，都是「数得清 / 查得到」的部分：
 *
 * 1. 描述里的「共 N 个端点」等于 `api/surface.json` 的端点数。
 * 2. 描述正文里提到的每个 `METHOD /path` 都真的是契约里的端点。
 *
 * 第 2 条是第五轮 review 补的：只判数字时，一句「发布走 `POST /admin/demos/{slug}/channels`」
 * 可以长期指向一个**不存在的端点**——数字对得上，读的人照着去调，得到 404。
 * 那正是 2026-11-04 那条 CRITICAL 的形状（契约指向了不存在的上架端点）。
 *
 * 它仍然证明不了「分组说明与实现语义一致」——那只能靠 review。
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

  // 描述里形如 `POST /api/v1/demos` 或 `POST /admin/demos/{slug}/channels` 的引用，
  // 必须能在 surface.json 的端点表里找到。允许省略前缀：文档里常写相对路径。
  const REF = /`(GET|POST|PUT|DELETE|PATCH)\s+(\/[\w/{}\-.]*)`/g;
  const known = Object.keys(surface?.endpoints ?? {});
  // 契约外端点（预览与下载）是**刻意**不在契约里的：它们的响应体是 HTML / zip 字节，
  // 「所有端点返回 JSON」这条前提对它们不成立。描述里提到它们是对的，
  // 所以这里按名字白名单放行——白名单本身写在契约描述里那句「不在契约里的两个端点」中，
  // 改这里等于改那句，别让两边漂。
  const OUTSIDE = ['GET /download/{slug}', 'GET /preview/{slug}/{path}', 'GET /preview/{slug}/v{rev}/{path}'];
  let ref;
  REF.lastIndex = 0;
  while ((ref = REF.exec(text)) !== null) {
    const [, method, path] = ref;
    if (OUTSIDE.includes(`${method} ${path}`)) continue;
    const hit = known.some((k) => {
      const [km, kp] = k.split(' ');
      if (km !== method) return false;
      return kp === path || kp.endsWith(path.replace(/^\/api\/v1/, '')) || kp.endsWith(path);
    });
    if (!hit) {
      problems.push(
        `契约描述里引用了 \`${method} ${path}\`，但契约里没有这个端点：` +
          '读的人会照着去调，然后得到 404。',
      );
    }
  }
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
