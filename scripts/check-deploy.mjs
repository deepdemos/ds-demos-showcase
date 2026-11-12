/**
 * 部署配置的结构门禁：关键的反代路径、SPA 回退、上传上限、安全头、服务单元。
 *
 * # 为什么需要这个
 *
 * 2026-11-09 之前，仓库里**没有任何 nginx 配置**。生产能跑起来靠的是当值的人记得住
 * 那几行——「/api/ 反代到 8080」「刷新深链要 try_files」「上传上限要够 200 MiB」。
 * 这类东西没法靠 code review 兜：它不是代码，谁都不读。
 *
 * 直接后果是那次预览 404：本地用 Vite dev server，生产用 nginx，两条路径的差异
 * （SPA 回退、前缀转发）没有任何东西在比。把配置收进仓库，至少让它和代码一起被读。
 *
 * # 判据
 *
 * 只判「结构还在」：
 *
 *   1. `deploy/nginx/deepdemos.conf` 里四段反代（/api/ /healthz /preview/ /download/）都在；
 *   2. SPA 回退 `try_files $uri $uri/ /index.html` 在；
 *   3. `client_max_body_size` 不小于契约允许的 200 MiB；
 *   4. `deploy/nginx/security-headers.conf` 有 CSP、nosniff、Referrer-Policy，
 *      且 CSP 含 `script-src 'self'`、`frame-ancestors`、`object-src 'none'`；
 *   5. `deploy/systemd/deepdemos-api.service` 有 EnvironmentFile 与 ExecStart。
 *
 * # 它不能证明什么
 *
 * 语法正确、能启动、头真的发出去、上传真的能过——**一条都证明不了**。
 * 开发机宿主与 WSL 的 rootfs 里都没有 `nginx`：这份 `deploy/nginx/deepdemos.conf` 从未被
 * `nginx -t` 加载过，真机反代一次都没跑过，只能靠 `docs/部署.md` §7 的验证清单在部署机上验。
 *
 * 容器那条路验过（WSL 里的 docker，`docs/部署.md` §10.4），它的 web 层同样是 nginx，但用的是另一份
 * 主配置（`deploy/docker/nginx.conf.template`，与这份共用 `deploy/nginx/security-headers.conf`）。
 * 容器跑通只证明那份模板与共用的安全头能用，不证明这份主配置能用。
 *
 * 所以这道门禁的定位是「防止配置被整段删掉或改歪」，不是「证明部署可用」。
 */
import { existsSync, readFileSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const ROOT = resolve(dirname(fileURLToPath(import.meta.url)), '..');
export const NGINX = join(ROOT, 'deploy', 'nginx', 'deepdemos.conf');
export const HEADERS = join(ROOT, 'deploy', 'nginx', 'security-headers.conf');
export const UNIT = join(ROOT, 'deploy', 'systemd', 'deepdemos-api.service');

/** 契约允许 200 MiB 的压缩包；nginx 侧不能比它小。 */
export const MAX_UPLOAD_BYTES = 200 * 1024 * 1024;

const PROXY_PATHS = ['/api/', '/healthz', '/preview/', '/download/'];

const CSP_MUST = ['script-src', 'frame-ancestors', "object-src 'none'", "default-src 'self'"];

/** 把 `client_max_body_size 220m;` 解析成字节数；看不懂就返回 0。 */
export function parseBodySize(text) {
  const m = text.match(/client_max_body_size\s+(\d+)\s*([kKmMgG]?)\s*;/);
  if (!m) return 0;
  const value = Number(m[1]);
  const unit = m[2].toLowerCase();
  const scale = unit === 'k' ? 1024 : unit === 'm' ? 1024 * 1024 : unit === 'g' ? 1024 ** 3 : 1;
  return value * scale;
}

export function checkDeploy({ nginx, headers, unit }) {
  const problems = [];

  for (const path of PROXY_PATHS) {
    const block = new RegExp(`location\\s+(?:=\\s+)?${path.replace(/[/]/g, '\\/')}`)
    if (!block.test(nginx)) problems.push(`deepdemos.conf 里没有 ${path} 的反代段（那个前缀会 404）`);
  }

  if (!/try_files\s+\$uri\s+\$uri\/\s+\/index\.html\s*;/.test(nginx)) {
    problems.push('deepdemos.conf 里没有 SPA 回退 try_files $uri $uri/ /index.html（刷新深链会 404）');
  }

  const size = parseBodySize(nginx);
  if (size < MAX_UPLOAD_BYTES) {
    problems.push(
      `deepdemos.conf 的 client_max_body_size 解析为 ${size} 字节，小于契约允许的 ${MAX_UPLOAD_BYTES}（200 MiB）`,
    );
  }

  if (!/include\s+\S*deepdemos-security-headers\.conf/.test(nginx)) {
    problems.push('deepdemos.conf 没有 include 安全头文件（location 里的 add_header 会整体覆盖 server 级）');
  }

  if (!/Content-Security-Policy/i.test(headers)) problems.push('security-headers.conf 里没有 Content-Security-Policy');
  if (!/X-Content-Type-Options\s+"?nosniff/i.test(headers)) problems.push('security-headers.conf 里没有 nosniff');
  if (!/Referrer-Policy/i.test(headers)) problems.push('security-headers.conf 里没有 Referrer-Policy');
  for (const part of CSP_MUST) {
    if (!headers.includes(part)) problems.push(`CSP 里缺少 ${part}`);
  }

  if (!/^EnvironmentFile=/m.test(unit)) problems.push('systemd 单元没有 EnvironmentFile=（密钥只能从那来）');
  if (!/^ExecStart=/m.test(unit)) problems.push('systemd 单元没有 ExecStart=');
  if (!/^\[Install\]/m.test(unit) || !/^WantedBy=/m.test(unit)) {
    problems.push('systemd 单元缺少 [Install] WantedBy=（enable 会失败）');
  }

  return { ok: problems.length === 0, problems };
}

function readOrExit(path, label) {
  if (!existsSync(path)) {
    console.error(`部署门禁失败：找不到${label} ${path}`);
    process.exit(1);
  }
  return readFileSync(path, 'utf8');
}

function main() {
  const result = checkDeploy({
    nginx: readOrExit(NGINX, 'nginx 配置'),
    headers: readOrExit(HEADERS, '安全头文件'),
    unit: readOrExit(UNIT, 'systemd 单元'),
  });
  if (!result.ok) {
    console.error('部署门禁失败：');
    for (const p of result.problems) console.error(`  - ${p}`);
    process.exit(1);
  }
  console.log('部署门禁通过（四段反代、SPA 回退、上传上限、安全头、服务单元都在）');
}

if (process.argv[1] && resolve(process.argv[1]) === resolve(fileURLToPath(import.meta.url))) {
  main();
}
