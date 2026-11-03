import { test } from 'node:test';
import assert from 'node:assert/strict';
import { layerOf, internalImports, violation } from './check-layering.mjs';

const MOD = 'github.com/deepdemos/ds-demos-showcase/backend/';

test('层判定：cmd / platform / httpapi / domain 子包 / 生成物', () => {
  assert.equal(layerOf('backend/cmd/api/main.go'), 'cmd');
  assert.equal(layerOf('backend/internal/platform/database/database.go'), 'platform');
  assert.equal(layerOf('backend/internal/httpapi/handlers.go'), 'httpapi');
  assert.equal(layerOf('backend/internal/domain/demo/service.go'), 'domain:demo');
  assert.equal(layerOf('backend/internal/kernel/verification/verification.go'), 'kernel:verification');
  assert.equal(layerOf('backend/internal/api/server.gen.go'), 'api');
  assert.equal(layerOf('backend/internal/store/db.go'), 'store');
  assert.equal(layerOf('docs/readme.md'), null);
});

test('层判定：反斜杠路径也认（Windows 上跑出来的路径）', () => {
  assert.equal(layerOf('backend\\internal\\httpapi\\router.go'), 'httpapi');
});

test('抽 import：只取本仓库内的，块式与单行都认', () => {
  const src = `package x

import (
	"fmt"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

import "${MOD}internal/platform/config"
`;
  assert.deepEqual(internalImports(src), ['internal/api', 'internal/store', 'internal/platform/config']);
});

test('抽 import：外部库不算', () => {
  const src = `package x

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)
`;
  assert.deepEqual(internalImports(src), []);
});

test('L1：platform 不得依赖业务 / 传输 / 生成物', () => {
  assert.match(violation('platform', 'domain:demo'), /基础设施一旦知道业务/);
  assert.match(violation('platform', 'httpapi'), /基础设施一旦知道业务/);
  assert.match(violation('platform', 'api'), /基础设施一旦知道业务/);
  assert.equal(violation('platform', 'platform'), null);
});

test('L2：domain 不得依赖传输层与生成的路由类型', () => {
  assert.match(violation('domain:demo', 'httpapi'), /业务逻辑不该知道 HTTP/);
  assert.match(violation('domain:demo', 'api'), /业务逻辑不该知道 HTTP/);
  assert.equal(violation('domain:demo', 'platform'), null);
  assert.equal(violation('domain:demo', 'store'), null);
});

test('L3：域之间不许互相 import，自己 import 自己不算', () => {
  assert.match(violation('domain:demo', 'domain:tag'), /域之间只能通过 platform/);
  assert.equal(violation('domain:demo', 'domain:demo'), null);
});

test('L6：kernel 不得依赖业务域 / 传输层 / 生成的路由类型', () => {
  assert.match(violation('kernel:verification', 'domain:demo'), /kernel 是所有域共用的机制/);
  assert.match(violation('kernel:verification', 'httpapi'), /机制不该知道 HTTP/);
  assert.match(violation('kernel:verification', 'api'), /机制不该知道 HTTP/);
  assert.equal(violation('kernel:verification', 'platform'), null);
  assert.equal(violation('kernel:verification', 'store'), null);
});

test('L6：kernel 子包之间不许互相 import，自己 import 自己不算', () => {
  assert.match(violation('kernel:verification', 'kernel:token'), /kernel 子包各自独立/);
  assert.equal(violation('kernel:verification', 'kernel:verification'), null);
});

test('kernel 的位置：domain 能用它，platform 不能用它', () => {
  // 这正是它存在的理由——机制放在 kernel，所有域都能 import。
  assert.equal(violation('domain:demo', 'kernel:verification'), null);
  // 反过来不行：基础设施不该知道 kernel。
  assert.match(violation('platform', 'kernel:verification'), /基础设施一旦知道业务/);
  assert.match(violation('store', 'kernel:verification'), /生成物不该 import/);
});

test('L4：生成物不得 import 本仓库其它包', () => {
  assert.match(violation('api', 'httpapi'), /生成物不该 import/);
  assert.match(violation('store', 'domain:demo'), /生成物不该 import/);
  assert.equal(violation('api', 'api'), null);
});

test('L5：cmd 与 httpapi 不受方向限制', () => {
  for (const to of ['platform', 'domain:demo', 'kernel:verification', 'httpapi', 'api', 'store']) {
    assert.equal(violation('cmd', to), null, `cmd → ${to} 应允许`);
    assert.equal(violation('httpapi', to), null, `httpapi → ${to} 应允许`);
  }
});
