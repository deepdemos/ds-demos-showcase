/**
 * 容器化部署的结构门禁：compose 与两个 Dockerfile 的关键约束还在不在。
 *
 * # 为什么单独一道
 *
 * `check-deploy.mjs` 管的是「宿主机装 nginx + systemd」那条路。容器这条路是另一套
 * 文件、另一组失效方式：数据卷挂错位置会让 postgres 拒绝启动、`api` 泄露端口会绕过
 * 反代的安全头、基础镜像跑 root 会让容器逃逸的代价变大。这些都不是代码，review 不看。
 *
 * # 判据（只判结构，不判能不能起来）
 *
 *   1. compose 里有 db / api / web 三个服务；
 *   2. api 与 db **不发布端口**（反代与 API 同一网络，暴露出去等于绕过安全头与限流）；
 *   3. api 依赖 db 的 healthy（不是 started——迁移会撞在还没准备好的库上）；
 *   4. 存储卷挂在 /app/var/storage（挂错位置 = 换容器作品文件全丢）；
 *   5. postgres 数据卷挂在 /var/lib/postgresql（18 起数据在子目录，挂 .../data 会被判成旧数据）；
 *   6. api 镜像有非 root 的 USER 与 ENTRYPOINT（启动时要先跑迁移）；
 *   7. 有 caddy 侧车时它必须是 `profiles: ["tls"]`、挂 Caddyfile、发布 443
 *      （不分档起就等于让每个部署都强制要域名与证书）。
 *
 * # 它不能证明什么
 *
 * 镜像能不能构建、容器能不能起来、迁移跑不跑得通、反代头对不对——一条都证明不了。
 * 那些只能靠 `docs/部署.md` 的验证清单在真机上跑。
 */
import { existsSync, readFileSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const ROOT = resolve(dirname(fileURLToPath(import.meta.url)), '..');
export const COMPOSE = join(ROOT, 'docker-compose.yml');
export const API_DOCKERFILE = join(ROOT, 'deploy', 'docker', 'api.Dockerfile');
export const WEB_DOCKERFILE = join(ROOT, 'deploy', 'docker', 'web.Dockerfile');

/**
 * 取 compose 里某个服务的配置块（按两格缩进的服务名切）。
 * 只做结构判断，不引入 YAML 解析器。
 */
function serviceBlock(compose, name) {
  return compose.split(new RegExp(`^ {2}${name}:\\s*$`, 'm'))[1]?.split(/^ {2}\w+:\s*$/m)[0] ?? '';
}

/** 只做结构判断，不引入 YAML 解析器（多一个依赖就多一份升级负担）。 */
export function checkDockerDeploy({ compose, apiDockerfile, webDockerfile }) {
  const problems = [];

  for (const service of ['db', 'api', 'web']) {
    if (!new RegExp(`^ {2}${service}:\\s*$`, 'm').test(compose)) {
      problems.push(`compose 里缺少 ${service} 服务`);
    }
  }

  // TLS 侧车：可选，但一旦存在就必须是分档的，且证书与 443 都得走它。
  const caddyBlock = serviceBlock(compose, 'caddy');
  if (caddyBlock.trim() !== '') {
    if (!/profiles:\s*\[\s*"tls"\s*\]/m.test(caddyBlock)) {
      problems.push('caddy 侧车必须写成 profiles: ["tls"]（否则每个部署都被强制要域名与证书）');
    }
    if (!/Caddyfile/.test(caddyBlock)) {
      problems.push('caddy 侧车没有挂 Caddyfile（站点与反向代理会全空）');
    }
    if (!/TLS_HTTPS_PORT:-443\}:443|"443:443"/.test(caddyBlock)) {
      problems.push('caddy 侧车没有发布 443（浏览器只认 443 上的 __Host- cookie）');
    }
  }

  // api 段里不能出现 ports:（web 才对外）
  const apiBlock = compose.split(/^ {2}api:\s*$/m)[1]?.split(/^ {2}\w+:\s*$/m)[0] ?? '';
  if (/^\s+ports:/m.test(apiBlock)) {
    problems.push('compose 的 api 服务发布了端口（会绕过反代的安全头与限流）');
  }
  const dbBlock = compose.split(/^ {2}db:\s*$/m)[1]?.split(/^ {2}\w+:\s*$/m)[0] ?? '';
  if (/^\s+ports:/m.test(dbBlock)) {
    problems.push('compose 的 db 服务发布了端口（数据库不该对公网开放）');
  }

  if (!/condition:\s*service_healthy/.test(compose)) {
    problems.push('compose 没有 depends_on: condition: service_healthy（迁移会撞在没准备好的库上）');
  }

  if (!/storage:\/app\/var\/storage/.test(compose)) {
    problems.push('compose 没把存储卷挂到 /app/var/storage（作品文件会随容器消失）');
  }

  if (!/db-data:\/var\/lib\/postgresql(\s|$)/m.test(compose)) {
    problems.push('postgres 数据卷应挂在 /var/lib/postgresql（18 起数据在子目录，挂 .../data 会被判成旧数据）');
  }

  if (!/^USER\s+(?!root)\S+/m.test(apiDockerfile)) {
    problems.push('api 镜像没有非 root 的 USER');
  }
  if (!/^ENTRYPOINT\s+/m.test(apiDockerfile)) {
    problems.push('api 镜像没有 ENTRYPOINT（启动要先跑迁移）');
  }
  if (!/nginx\.conf\.template/.test(webDockerfile)) {
    problems.push('web 镜像没有把 nginx.conf.template 放进 templates 目录（上游地址会写死）');
  }

  return { ok: problems.length === 0, problems };
}

function readOrExit(path, label) {
  if (!existsSync(path)) {
    console.error(`容器部署门禁失败：找不到${label} ${path}`);
    process.exit(1);
  }
  return readFileSync(path, 'utf8');
}

function main() {
  const result = checkDockerDeploy({
    compose: readOrExit(COMPOSE, 'compose 文件'),
    apiDockerfile: readOrExit(API_DOCKERFILE, 'api 镜像定义'),
    webDockerfile: readOrExit(WEB_DOCKERFILE, 'web 镜像定义'),
  });
  if (!result.ok) {
    console.error('容器部署门禁失败：');
    for (const p of result.problems) console.error(`  - ${p}`);
    process.exit(1);
  }
  console.log('容器部署门禁通过（三个服务、端口不泄露、健康依赖、两处数据卷、非 root、TLS 侧车分档）');
}

if (process.argv[1] && resolve(process.argv[1]) === resolve(fileURLToPath(import.meta.url))) {
  main();
}
