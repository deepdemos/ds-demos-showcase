// file-size 门禁里「第三方资产按哈希放行」这一条的自测。
// 单独成文件：`checks.test.mjs` 已经接近行数上限，而这些用例与其它门禁无关。
import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { test } from 'node:test';

import { VENDORED, collectOversizedFiles, vendoredProblems } from './check-file-size.mjs';

const VENDORED_PATH = [...VENDORED.keys()][0];

test('第三方资产按哈希放行，不计入行数', () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'vendored-'));
  try {
    execFileSync('git', ['init', '-q'], { cwd: dir });
    fs.mkdirSync(path.join(dir, path.dirname(VENDORED_PATH)), { recursive: true });
    fs.writeFileSync(path.join(dir, VENDORED_PATH), 'x\n'.repeat(1200));
    fs.writeFileSync(path.join(dir, 'handwritten.ts'), 'x\n'.repeat(1200));
    execFileSync('git', ['add', '.'], { cwd: dir });

    assert.deepEqual(
      collectOversizedFiles(dir).map((row) => row.file),
      ['handwritten.ts'],
      '第三方资产不该出现在行数清单里，手写文件仍要出现',
    );
  } finally {
    fs.rmSync(dir, { recursive: true, force: true });
  }
});

test('第三方资产内容被改过要红', () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'vendored-'));
  try {
    fs.mkdirSync(path.join(dir, path.dirname(VENDORED_PATH)), { recursive: true });
    fs.writeFileSync(path.join(dir, VENDORED_PATH), '// 被人手改过\n');

    const problems = vendoredProblems(dir);
    assert.equal(problems.length, 1);
    assert.match(problems[0], /内容与登记不符/);
  } finally {
    fs.rmSync(dir, { recursive: true, force: true });
  }
});

test('登记的第三方资产不存在也要红', () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'vendored-'));
  try {
    const problems = vendoredProblems(dir);
    assert.equal(problems.length, 1);
    assert.match(problems[0], /仓库里没有这个文件/);
  } finally {
    fs.rmSync(dir, { recursive: true, force: true });
  }
});

test('仓库里真实的第三方资产与登记一致', () => {
  assert.deepEqual(vendoredProblems(), [], '真实仓库里的第三方资产必须对得上登记的哈希');
});
