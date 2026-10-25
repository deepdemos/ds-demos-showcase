import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { test } from 'node:test';
import { classifyLines, countTextLines, listSourceFiles } from './check-file-size.mjs';
import { validatePullRequest } from './check-pr.mjs';

test('文件行数阈值：500/501/800/801', () => {
  assert.equal(classifyLines(500), 'ok');
  assert.equal(classifyLines(501), 'warning');
  assert.equal(classifyLines(800), 'warning');
  assert.equal(classifyLines(801), 'error');
});

test('行数统计兼容 CRLF 与末尾无换行', () => {
  assert.equal(countTextLines('a\nb\n'), 2);
  assert.equal(countTextLines('a\r\nb\r\n'), 2);
  assert.equal(countTextLines('a\nb'), 2);
});

test('支持中文路径（core.quotePath=false）', () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'file-size-'));
  try {
    execFileSync('git', ['init', '-q'], { cwd: dir });
    fs.mkdirSync(path.join(dir, '中文'));
    fs.writeFileSync(path.join(dir, '中文', '测试.ts'), 'x\n'.repeat(501));
    execFileSync('git', ['add', '.'], { cwd: dir });
    assert.ok(listSourceFiles(dir).includes('中文/测试.ts'));
  } finally {
    fs.rmSync(dir, { recursive: true, force: true });
  }
});

test('PR 门禁：合法输入', () => {
  assert.deepEqual(
    validatePullRequest({
      title: 'feat(scope): 描述',
      body: 'Closes #47',
      additions: 500,
      deletions: 0,
      changed_files: 20,
    }),
    [],
  );
  assert.deepEqual(
    validatePullRequest({
      title: 'fix: 修复',
      body: '- Fixes #47',
      additions: 10,
      deletions: 5,
      changed_files: 2,
    }),
    [],
  );
});

test('PR 门禁：无 issue / 标题不合法 / 超规模', () => {
  const errors = validatePullRequest({
    title: 'update stuff',
    body: 'no issue',
    additions: 501,
    deletions: 0,
    changed_files: 21,
  });
  assert.equal(errors.length, 4);
  assert.ok(errors.some((error) => error.includes('issue')));
  assert.ok(errors.some((error) => error.includes('标题')));
  assert.ok(errors.some((error) => error.includes('行上限')));
  assert.ok(errors.some((error) => error.includes('文件上限')));
});

test('PR 门禁：字段缺失 fail-closed', () => {
  const errors = validatePullRequest({ title: 'feat: 描述', body: 'Closes #47' });
  assert.ok(errors.some((error) => error.includes('缺少')));
});

test('PR 门禁：issue 编号不能为 0', () => {
  const errors = validatePullRequest({
    title: 'feat: 描述',
    body: 'Closes #0',
    additions: 1,
    deletions: 0,
    changed_files: 1,
  });
  assert.ok(errors.some((error) => error.includes('issue')));
});

test('PR 门禁：代码块里的 issue 关键字不算关联', () => {
  const errors = validatePullRequest({
    title: 'feat: 描述',
    body: '说明：```text\nCloses #47\n```',
    additions: 1,
    deletions: 0,
    changed_files: 1,
  });
  assert.ok(errors.some((error) => error.includes('issue')));
});
