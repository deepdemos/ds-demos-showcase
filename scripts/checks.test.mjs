import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { test } from 'node:test';
import { classifyLines, countTextLines, listSourceFiles } from './check-file-size.mjs';
import { isDocsOnly, validatePullRequest } from './check-pr.mjs';
import { hasReviewComment } from './check-review.mjs';
import { inspectDocument as inspectStandardObjects, inspectYamlFences, parseAuthority as parseStandardObjects, run as runStandardObjects } from './check-objects.mjs';

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

test('isDocsOnly：拒绝越界路径段', () => {
  assert.equal(isDocsOnly(['docs/../backend/main.go']), false);
  assert.equal(isDocsOnly(['docs/a.md', '../secrets.md']), false);
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
const AUTHORITY_TEXT = [
  '## 登记表',
  '| `ST-1` | 账号状态 | 身份域 | 全部 | **已展开** |',
  '## §ST-1 账号状态 `AccountStatus`',
  '```sql',
  "CHECK (status IN ('pending', 'active', 'suspended', 'banned', 'deleted'))",
  '```',
].join('\n');

test('标准对象门禁：完整抄取值域必须带锚点', () => {
  const authority = parseStandardObjects(AUTHORITY_TEXT);
  const copied = "```sql\nCHECK (status IN ('pending','active','suspended','banned','deleted'))\n```\n";
  assert.ok(inspectStandardObjects('a.md', copied, authority).some((p) => p.includes('缺')));
  assert.deepEqual(
    inspectStandardObjects('a.md', `见 标准对象 §ST-1。\n\n${copied}`, authority),
    [],
  );
});

test('标准对象门禁：取值写歪了要报，私有枚举放行', () => {
  const authority = parseStandardObjects(AUTHORITY_TEXT);
  const drifted = "标准对象 §ST-1\nCHECK (status IN ('active','suspended','deleted'))\n";
  assert.ok(inspectStandardObjects('a.md', drifted, authority).some((p) => p.includes('不一致')));
  assert.deepEqual(inspectStandardObjects('a.md', "CHECK (status IN ('ok','error'))\n", authority), []);
});

test('标准对象门禁：引用键格式与登记表', () => {
  const authority = parseStandardObjects(AUTHORITY_TEXT);
  assert.ok(inspectStandardObjects('a.md', '见 标准对象 §XX-9\n', authority).some((p) => p.includes('格式')));
  assert.ok(inspectStandardObjects('a.md', '见 标准对象 §EN-99\n', authority).some((p) => p.includes('未登记')));
  assert.deepEqual(inspectStandardObjects('a.md', '见 标准对象 §ST-1\n', authority), []);
});

test('标准对象门禁：真实文档全绿', () => {
  const { problems, objects } = runStandardObjects();
  assert.deepEqual(problems, []);
  assert.ok(objects.length >= 1);
});

const DERIVED_AUTHORITY = [
  '| `ST-9` | 派生档位 | 一 | 派生 |',
  '## §ST-9 派生档位',
  '取值域：`a1` / `a2` / `a3`',
].join('\n');

test('标准对象门禁：派生对象（无 CHECK）靠「取值域：」声明识别', () => {
  const authority = parseStandardObjects(DERIVED_AUTHORITY);
  assert.equal(authority.objects.length, 1);
  assert.equal(authority.objects[0].column, null);
  const copied = '档位：`a1` / `a2` / `a3` 全量复制\n';
  assert.ok(inspectStandardObjects('a.md', copied, authority).some((p) => p.includes('缺')));
  assert.deepEqual(inspectStandardObjects('a.md', `见 标准对象 §ST-9。\n\n${copied}`, authority), []);
});

test('标准对象门禁：派生对象的局部枚举不算复制（不误伤行文）', () => {
  const authority = parseStandardObjects(DERIVED_AUTHORITY);
  assert.deepEqual(inspectStandardObjects('a.md', '作者可见：`a1` / `a2`\n', authority), []);
  assert.deepEqual(inspectStandardObjects('a.md', '混排：`a1` / `a2` / `zzz`\n', authority), []);
});

test('标准对象门禁：章节级锚点必须写在副本之前', () => {
  const authority = parseStandardObjects(DERIVED_AUTHORITY);
  // 锚点远在副本下方（超出 ±4 行窗口、也不在章节前缀里）→ 仍要报
  const filler = '\n'.repeat(8);
  const copyLine = '档位：`a1` / `a2` / `a3`';
  const after = '## 某节\n' + copyLine + '\n' + filler + '见 标准对象 §ST-9。\n';
  assert.ok(inspectStandardObjects('a.md', after, authority).some((p) => p.includes('缺')));
});

test('标准对象门禁：YAML 围栏顶层结构混用要被拦（映射键 + 序列项同级）', () => {
  const mixed = ['```yaml', 'version: 1', 'not_content_tables: [tasks]', '- table: demos', '```', ''].join('\n');
  const problems = inspectYamlFences(mixed);
  assert.equal(problems.length, 1);
  assert.ok(problems[0].includes('混用'));

  const clean = ['```yaml', 'version: 1', 'tables:', '  - a', 'not_content_tables:', '  - tasks', '```', ''].join('\n');
  assert.deepEqual(inspectYamlFences(clean), []);

  // 纯序列、带注释、带 --- 也算干净；非 yaml 围栏不参与
  assert.deepEqual(inspectYamlFences(['```yaml', '# 注释', '- a', '- b', '```'].join('\n')), []);
  assert.deepEqual(inspectYamlFences(['```sql', 'not_content_tables: [x]', '- y', '```'].join('\n')), []);
});
