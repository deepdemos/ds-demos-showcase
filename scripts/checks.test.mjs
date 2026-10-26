import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { test } from 'node:test';
import { classifyLines, countTextLines, listSourceFiles } from './check-file-size.mjs';
import { isDocsOnly, validatePullRequest } from './check-pr.mjs';
import { hasReviewComment } from './check-review.mjs';

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

test('PR 门禁：纯文档 PR 豁免规模上限，但仍要 issue 与合法标题', () => {
  const docsFiles = ['docs/模型域设计.md', 'docs/Demo域设计.md'];
  assert.equal(isDocsOnly(docsFiles), true);
  assert.deepEqual(
    validatePullRequest({
      title: 'docs(model): 设计文档入库',
      body: 'Refs #100',
      additions: 1800,
      deletions: 0,
      changed_files: 5,
      files: docsFiles,
    }),
    [],
  );
  const noIssue = validatePullRequest({
    title: 'docs(model): 设计文档入库',
    body: '无关联',
    additions: 1800,
    deletions: 0,
    changed_files: 5,
    files: docsFiles,
  });
  assert.ok(noIssue.some((error) => error.includes('issue')));
});

test('isDocsOnly：混入非 docs 文件或空清单都不豁免', () => {
  assert.equal(isDocsOnly(['docs/a.md', 'scripts/check-pr.mjs']), false);
  assert.equal(isDocsOnly(['README.md']), false);
  assert.equal(isDocsOnly([]), false);
  assert.equal(isDocsOnly(undefined), false);
});

test('PR 门禁：文件清单混入代码时恢复规模校验', () => {
  const errors = validatePullRequest({
    title: 'docs(model): 设计文档入库',
    body: 'Refs #100',
    additions: 600,
    deletions: 0,
    changed_files: 2,
    files: ['docs/a.md', 'backend/main.go'],
  });
  assert.ok(errors.some((error) => error.includes('行上限')));
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

test('review 锁：评论必须提交、非空且晚于最新 commit', () => {
  const commit = '2026-10-25T10:00:00Z';
  const longBody = '## 审查结论\n无阻断项，建议合并。';
  assert.equal(hasReviewComment([], commit), false);
  assert.equal(hasReviewComment([{ state: 'COMMENTED', body: longBody }], commit), false);
  assert.equal(
    hasReviewComment(
      [{ state: 'COMMENTED', body: longBody, submitted_at: '2026-10-25T09:59:59Z' }],
      commit,
    ),
    false,
  );
  assert.equal(
    hasReviewComment(
      [{ state: 'COMMENTED', body: longBody, submitted_at: '2026-10-25T10:00:00Z' }],
      commit,
    ),
    false,
  );
  assert.equal(
    hasReviewComment(
      [{ state: 'COMMENTED', body: longBody, submitted_at: '2026-10-25T10:00:01Z' }],
      commit,
    ),
    true,
  );
  assert.equal(
    hasReviewComment(
      [{ state: 'PENDING', body: longBody, submitted_at: '2026-10-25T10:00:01Z' }],
      commit,
    ),
    false,
  );
  assert.equal(
    hasReviewComment(
      [{ state: 'COMMENTED', body: 'LGTM', submitted_at: '2026-10-25T10:00:01Z' }],
      commit,
    ),
    false,
  );
  assert.equal(hasReviewComment([{ state: 'COMMENTED', body: longBody }], undefined), false);
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