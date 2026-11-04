import assert from 'node:assert/strict';
import { mkdtempSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { test } from 'node:test';

import { audit, danglingReferences } from './weekly-audit.mjs';

/**
 * 每周自查的自测。
 *
 * 重点不在渲染好不好看，而在两件会导致误报的事：
 * ① 删除线里的旧路径不该算失效（`~~旧路径~~ → 新路径` 是历史记录）；
 * ② 从子模块写的相对路径（`internal/x.go` 而不是 `backend/internal/x.go`）不该算失效。
 *
 * 误报一次，这个自查就会被当成噪音关掉——和没有它是一样的。
 */
function docFile(markdown) {
  const dir = mkdtempSync(join(tmpdir(), 'weekly-audit-'));
  const path = join(dir, 'loose.md');
  writeFileSync(path, markdown);
  return { path, cleanup: () => rmSync(dir, { recursive: true, force: true }) };
}

test('删除线里的旧路径不算失效引用', () => {
  const f = docFile(
    [
      '## 1. 某个已经修掉的松点',
      '',
      '| 在哪 | ~~`backend/internal/api/api.gen.go`~~ → 已拆成 `types.gen.go` |',
      '',
    ].join('\n'),
  );
  try {
    assert.deepEqual(danglingReferences(f.path), []);
  } finally {
    f.cleanup();
  }
});

test('引用了不存在的文件：算失效', () => {
  const f = docFile(['## 2. 某个松点', '', '判定写在 `backend/internal/nope/gone.go` 里。', ''].join('\n'));
  try {
    const gone = danglingReferences(f.path);
    assert.equal(gone.length, 1);
    assert.equal(gone[0].n, 2);
    assert.equal(gone[0].token, 'backend/internal/nope/gone.go');
  } finally {
    f.cleanup();
  }
});

test('子模块相对路径（internal/…）不该被当成失效', () => {
  const f = docFile(['## 3. 某个松点', '', '见 `internal/domain/identity/grant.go`。', ''].join('\n'));
  try {
    assert.deepEqual(danglingReferences(f.path), []);
  } finally {
    f.cleanup();
  }
});

test('行号后缀会被剥掉再判存在性', () => {
  const f = docFile(['## 4. 某个松点', '', '见 `backend/internal/httpapi/download.go:143`。', ''].join('\n'));
  try {
    assert.deepEqual(danglingReferences(f.path), []);
  } finally {
    f.cleanup();
  }
});

test('门禁全绿时 audit 不报问题', () => {
  const f = docFile(['## 1. 一条松点', '', '没有引用。', ''].join('\n'));
  try {
    const result = audit({ runGateFn: (script) => ({ script, ok: true, output: '' }), docPath: f.path });
    assert.equal(result.failed.length, 0);
    assert.deepEqual(result.dangling, []);
    assert.equal(result.looseCount, 1);
  } finally {
    f.cleanup();
  }
});

test('门禁红时 audit 把它记进 failed（自查要能发现门禁自己坏了）', () => {
  const f = docFile(['## 1. 一条松点', '', '没有引用。', ''].join('\n'));
  try {
    const result = audit({
      runGateFn: (script) => ({ script, ok: script !== 'scripts/check-negative-cases.mjs', output: '端点缺反例' }),
      docPath: f.path,
    });
    assert.equal(result.failed.length, 1);
    assert.equal(result.failed[0].script, 'scripts/check-negative-cases.mjs');
  } finally {
    f.cleanup();
  }
});

test('真实仓库当前没有失效引用', () => {
  assert.deepEqual(danglingReferences(), []);
});
