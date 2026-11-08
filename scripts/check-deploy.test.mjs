import assert from 'node:assert/strict';
import { test } from 'node:test';

import { checkDeploy, parseBodySize, MAX_UPLOAD_BYTES } from './check-deploy.mjs';

/**
 * 部署门禁的自测。
 *
 * 门禁本身只判「结构与关键行还在」，所以用例都是「造一份缺了一项的配置，看它报不报」。
 * 每个用例只坏一处，避免一条用例同时证明两件事。
 */

const goodNginx = `
upstream deepdemos_api { server 127.0.0.1:8080; }

server {
    client_max_body_size 220m;
    include /etc/nginx/conf.d/deepdemos-security-headers.conf;

    location /api/ { proxy_pass http://deepdemos_api; }
    location = /healthz { proxy_pass http://deepdemos_api; }
    location /preview/ { proxy_pass http://deepdemos_api; }
    location /download/ { proxy_pass http://deepdemos_api; }
    location / { try_files $uri $uri/ /index.html; }
}
`;

const goodHeaders = `
add_header Content-Security-Policy "default-src 'self'; script-src 'self'; object-src 'none'; frame-ancestors 'none'" always;
add_header X-Content-Type-Options "nosniff" always;
add_header Referrer-Policy "strict-origin-when-cross-origin" always;
`;

const goodUnit = `
[Unit]
Description=x

[Service]
EnvironmentFile=/etc/deepdemos/api.env
ExecStart=/usr/local/bin/deepdemos-api

[Install]
WantedBy=multi-user.target
`;

const run = (over = {}) =>
  checkDeploy({
    nginx: over.nginx ?? goodNginx,
    headers: over.headers ?? goodHeaders,
    unit: over.unit ?? goodUnit,
  });

test('三份配置齐全 → 绿', () => {
  const r = run();
  assert.equal(r.ok, true, r.problems.join('; '));
});

test('少了 /api/ 反代段 → 红，并点出是哪个前缀', () => {
  const r = run({ nginx: goodNginx.replace('location /api/ { proxy_pass http://deepdemos_api; }', '') });
  assert.equal(r.ok, false);
  assert.ok(
    r.problems.some((p) => p.includes('/api/')),
    r.problems.join('; '),
  );
});

test('少了 SPA 回退 → 红（刷新深链会 404）', () => {
  const r = run({ nginx: goodNginx.replace('try_files $uri $uri/ /index.html;', 'try_files $uri =404;') });
  assert.equal(r.ok, false);
  assert.ok(
    r.problems.some((p) => p.includes('SPA 回退')),
    r.problems.join('; '),
  );
});

test('上传上限小于 200 MiB → 红，并报出实际解析到的字节数', () => {
  const r = run({ nginx: goodNginx.replace('client_max_body_size 220m;', 'client_max_body_size 1m;') });
  assert.equal(r.ok, false);
  const hit = r.problems.find((p) => p.includes('client_max_body_size'));
  assert.ok(hit, r.problems.join('; '));
  assert.ok(hit.includes(String(1024 * 1024)), hit);
});

test('CSP 缺 script-src → 红（等于允许任意脚本）', () => {
  const r = run({ headers: goodHeaders.replace("script-src 'self'; ", '') });
  assert.equal(r.ok, false);
  assert.ok(
    r.problems.some((p) => p.includes('script-src')),
    r.problems.join('; '),
  );
});

test('nginx 没有 include 安全头文件 → 红（location 里的 add_header 会覆盖 server 级）', () => {
  const r = run({ nginx: goodNginx.replace('include /etc/nginx/conf.d/deepdemos-security-headers.conf;', '') });
  assert.equal(r.ok, false);
  assert.ok(
    r.problems.some((p) => p.includes('安全头文件')),
    r.problems.join('; '),
  );
});

test('systemd 单元没有 EnvironmentFile → 红（密钥只能从那来）', () => {
  const r = run({ unit: goodUnit.replace('EnvironmentFile=/etc/deepdemos/api.env', '') });
  assert.equal(r.ok, false);
  assert.ok(
    r.problems.some((p) => p.includes('EnvironmentFile')),
    r.problems.join('; '),
  );
});

test('client_max_body_size 支持 k/m/g 三种单位', () => {
  assert.equal(parseBodySize('client_max_body_size 220m;'), 220 * 1024 * 1024);
  assert.equal(parseBodySize('client_max_body_size 512k;'), 512 * 1024);
  assert.equal(parseBodySize('client_max_body_size 1g;'), 1024 ** 3);
  assert.equal(parseBodySize('client_max_body_size 300000000;'), 300000000);
  assert.equal(parseBodySize('没有这一行'), 0);
  assert.equal(MAX_UPLOAD_BYTES, 200 * 1024 * 1024);
});
