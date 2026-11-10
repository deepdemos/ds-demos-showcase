import assert from 'node:assert/strict';
import { test } from 'node:test';

import { checkDockerDeploy } from './check-docker-deploy.mjs';

/**
 * 容器部署门禁的自测：每个用例只坏一处，避免一条用例同时证明两件事。
 */

const goodCompose = `
services:
  db:
    image: postgres:18-alpine
    healthcheck:
      test: ["CMD-SHELL", "pg_isready"]
    volumes:
      - db-data:/var/lib/postgresql

  api:
    build:
      context: .
    depends_on:
      db:
        condition: service_healthy
    volumes:
      - storage:/app/var/storage

  web:
    ports:
      - "8080:80"

volumes:
  db-data:
  storage:
`;

const goodApi = `
FROM golang:1.27-alpine AS build
FROM alpine:3.22
USER app
ENTRYPOINT ["/app/entrypoint.sh"]
`;

const goodWeb = `
FROM nginx:1.27-alpine
COPY deploy/docker/nginx.conf.template /etc/nginx/templates/default.conf.template
`;

const run = (over = {}) =>
  checkDockerDeploy({
    compose: over.compose ?? goodCompose,
    apiDockerfile: over.apiDockerfile ?? goodApi,
    webDockerfile: over.webDockerfile ?? goodWeb,
  });

test('三份文件齐全 → 绿', () => {
  const r = run();
  assert.equal(r.ok, true, r.problems.join('; '));
});

test('api 发布了端口 → 红（绕过反代的安全头）', () => {
  const compose = goodCompose.replace(
    '    depends_on:',
    '    ports:\n      - "8080:8080"\n    depends_on:',
  );
  const r = run({ compose });
  assert.equal(r.ok, false);
  assert.ok(
    r.problems.some((p) => p.includes('api 服务发布了端口')),
    r.problems.join('; '),
  );
});

test('依赖没有 condition: service_healthy → 红', () => {
  const compose = goodCompose.replace('        condition: service_healthy', '        condition: service_started');
  const r = run({ compose });
  assert.equal(r.ok, false);
  assert.ok(
    r.problems.some((p) => p.includes('service_healthy')),
    r.problems.join('; '),
  );
});

test('存储卷挂错位置 → 红（作品文件会随容器消失）', () => {
  const compose = goodCompose.replace('storage:/app/var/storage', 'storage:/app/storage');
  const r = run({ compose });
  assert.equal(r.ok, false);
  assert.ok(
    r.problems.some((p) => p.includes('/app/var/storage')),
    r.problems.join('; '),
  );
});

test('postgres 数据卷挂到 .../data → 红（18 起会被判成旧数据）', () => {
  const compose = goodCompose.replace('db-data:/var/lib/postgresql', 'db-data:/var/lib/postgresql/data');
  const r = run({ compose });
  assert.equal(r.ok, false);
  assert.ok(
    r.problems.some((p) => p.includes('postgres 数据卷')),
    r.problems.join('; '),
  );
});

test('api 镜像以 root 跑 → 红', () => {
  const r = run({ apiDockerfile: goodApi.replace('USER app', 'USER root') });
  assert.equal(r.ok, false);
  assert.ok(
    r.problems.some((p) => p.includes('非 root')),
    r.problems.join('; '),
  );
});

test('web 镜像没把模板放进 templates → 红（上游地址会写死）', () => {
  const r = run({ webDockerfile: goodWeb.replace('nginx.conf.template ', '') });
  assert.equal(r.ok, false);
  assert.ok(
    r.problems.some((p) => p.includes('nginx.conf.template')),
    r.problems.join('; '),
  );
});
