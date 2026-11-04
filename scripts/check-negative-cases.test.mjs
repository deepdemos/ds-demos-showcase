import assert from 'node:assert/strict';
import { mkdtempSync, mkdirSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { test } from 'node:test';

import { checkNegativeCases } from './check-negative-cases.mjs';

/**
 * 反例矩阵门禁的自测。
 *
 * 每条用例都造一个**假的仓库根**：一个 surface.json（伪契约）、一个 fake Go 测试文件、
 * 一份 negative-cases.json。这样能精确验「哪种写法该红、哪种该绿」，
 * 而不依赖真仓库当时正好有几个端点。
 */
function fixture({ surface, cases, goTests = {}, tsTests = {} }) {
  const root = mkdtempSync(join(tmpdir(), 'negcases-'));
  writeFileSync(join(root, 'surface.json'), JSON.stringify(surface));
  writeFileSync(join(root, 'cases.json'), JSON.stringify(cases));
  for (const [file, body] of Object.entries({ ...goTests, ...tsTests })) {
    const abs = join(root, file);
    mkdirSync(dirname(abs), { recursive: true });
    writeFileSync(abs, body);
  }
  return root;
}

const surface = (paths) => ({
  models: {},
  enums: {},
  endpoints: Object.fromEntries(paths.map((p) => [p, { method: 'X', pathParams: 0, params: null }])),
});

const goBody = (...names) => names.map((n) => `func ${n}(t *testing.T) {\n}\n`).join('\n');

test('全部齐备：通过', () => {
  const root = fixture({
    surface: surface(['GET /a']),
    goTests: { 'backend/a_test.go': goBody('TestARejects') },
    cases: {
      endpoints: { 'GET /a': [{ case: '必须拒绝什么什么', test: { file: 'backend/a_test.go', name: 'TestARejects' } }] },
      outsideContract: {},
    },
  });
  try {
    const { problems, stats } = checkNegativeCases({
      casesFile: join(root, 'cases.json'),
      surfaceFile: join(root, 'surface.json'),
      root,
      requiredOutside: [],
    });
    assert.deepEqual(problems, []);
    assert.equal(stats.cases, 1);
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test('契约端点没写反例：红', () => {
  const root = fixture({
    surface: surface(['GET /a', 'GET /b']),
    goTests: { 'backend/a_test.go': goBody('TestARejects') },
    cases: {
      endpoints: { 'GET /a': [{ case: '必须拒绝什么什么', test: { file: 'backend/a_test.go', name: 'TestARejects' } }] },
      outsideContract: {},
    },
  });
  try {
    const { problems } = checkNegativeCases({
      casesFile: join(root, 'cases.json'),
      surfaceFile: join(root, 'surface.json'),
      root,
      requiredOutside: [],
    });
    assert.ok(problems.some((p) => p.includes('契约端点没有反例：GET /b')), problems.join('\n'));
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test('矩阵登记了契约里没有的端点：红（删端点忘了收账）', () => {
  const root = fixture({
    surface: surface(['GET /a']),
    goTests: { 'backend/a_test.go': goBody('TestARejects') },
    cases: {
      endpoints: {
        'GET /a': [{ case: '必须拒绝什么什么', test: { file: 'backend/a_test.go', name: 'TestARejects' } }],
        'GET /gone': [{ case: '必须拒绝什么什么', test: { file: 'backend/a_test.go', name: 'TestARejects' } }],
      },
      outsideContract: {},
    },
  });
  try {
    const { problems } = checkNegativeCases({
      casesFile: join(root, 'cases.json'),
      surfaceFile: join(root, 'surface.json'),
      root,
      requiredOutside: [],
    });
    assert.ok(problems.some((p) => p.includes('契约里不存在的端点：GET /gone')), problems.join('\n'));
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test('指向的 Go 测试函数不存在：红', () => {
  const root = fixture({
    surface: surface(['GET /a']),
    goTests: { 'backend/a_test.go': goBody('TestSomethingElse') },
    cases: {
      endpoints: { 'GET /a': [{ case: '必须拒绝什么什么', test: { file: 'backend/a_test.go', name: 'TestMissing' } }] },
      outsideContract: {},
    },
  });
  try {
    const { problems } = checkNegativeCases({
      casesFile: join(root, 'cases.json'),
      surfaceFile: join(root, 'surface.json'),
      root,
      requiredOutside: [],
    });
    assert.ok(problems.some((p) => p.includes('没有测试函数 TestMissing')), problems.join('\n'));
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test('测试函数存在但只是同名前缀：不该被当成存在', () => {
  const root = fixture({
    surface: surface(['GET /a']),
    goTests: { 'backend/a_test.go': goBody('TestMissingButLonger') },
    cases: {
      endpoints: { 'GET /a': [{ case: '必须拒绝什么什么', test: { file: 'backend/a_test.go', name: 'TestMissing' } }] },
      outsideContract: {},
    },
  });
  try {
    const { problems } = checkNegativeCases({
      casesFile: join(root, 'cases.json'),
      surfaceFile: join(root, 'surface.json'),
      root,
      requiredOutside: [],
    });
    assert.ok(problems.some((p) => p.includes('没有测试函数 TestMissing')), problems.join('\n'));
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test('测试文件不存在：红', () => {
  const root = fixture({
    surface: surface(['GET /a']),
    cases: {
      endpoints: { 'GET /a': [{ case: '必须拒绝什么什么', test: { file: 'backend/nowhere_test.go', name: 'TestX' } }] },
      outsideContract: {},
    },
  });
  try {
    const { problems } = checkNegativeCases({
      casesFile: join(root, 'cases.json'),
      surfaceFile: join(root, 'surface.json'),
      root,
      requiredOutside: [],
    });
    assert.ok(problems.some((p) => p.includes('测试文件不存在：backend/nowhere_test.go')), problems.join('\n'));
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test('前端用例名也能认（test / it 两种写法）', () => {
  const root = fixture({
    surface: surface(['GET /a']),
    tsTests: { 'web/a.test.tsx': "test('404 时展示作品不存在', () => {});\nit('另一种写法', () => {});\n" },
    cases: {
      endpoints: {
        'GET /a': [
          { case: '不存在的作品要说明白', test: { file: 'web/a.test.tsx', name: '404 时展示作品不存在' } },
          { case: '另一种写法的用例', test: { file: 'web/a.test.tsx', name: '另一种写法' } },
        ],
      },
      outsideContract: {},
    },
  });
  try {
    const { problems } = checkNegativeCases({
      casesFile: join(root, 'cases.json'),
      surfaceFile: join(root, 'surface.json'),
      root,
      requiredOutside: [],
    });
    assert.deepEqual(problems, []);
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test('契约外端点漏了：红', () => {
  const root = fixture({
    surface: surface(['GET /a']),
    goTests: { 'backend/a_test.go': goBody('TestARejects') },
    cases: {
      endpoints: { 'GET /a': [{ case: '必须拒绝什么什么', test: { file: 'backend/a_test.go', name: 'TestARejects' } }] },
      outsideContract: {},
    },
  });
  try {
    const { problems } = checkNegativeCases({
      casesFile: join(root, 'cases.json'),
      surfaceFile: join(root, 'surface.json'),
      root,
      requiredOutside: ['GET /download/{slug}'],
    });
    assert.ok(problems.some((p) => p.includes('契约外端点没有反例：GET /download/{slug}')), problems.join('\n'));
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test('说明太短：红（防止写「todo」糊过去）', () => {
  const root = fixture({
    surface: surface(['GET /a']),
    goTests: { 'backend/a_test.go': goBody('TestARejects') },
    cases: {
      endpoints: { 'GET /a': [{ case: 'todo', test: { file: 'backend/a_test.go', name: 'TestARejects' } }] },
      outsideContract: {},
    },
  });
  try {
    const { problems } = checkNegativeCases({
      casesFile: join(root, 'cases.json'),
      surfaceFile: join(root, 'surface.json'),
      root,
      requiredOutside: [],
    });
    assert.ok(problems.some((p) => p.includes('反例说明太短或为空')), problems.join('\n'));
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test('当前仓库真的过这道门禁', () => {
  const { problems, stats } = checkNegativeCases();
  assert.deepEqual(problems, []);
  assert.ok(stats.endpoints >= 16, `端点数量看着不对：${stats.endpoints}`);
});
