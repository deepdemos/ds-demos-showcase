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
import { inspectContracts } from './check-contracts.mjs';

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

const CONTRACT_GOV = [
  '### 4.3 权限清单（2 个）',
  '',
  '| 域 | permission | 说明 | `moderator` | `admin` |',
  '|---|---|---|---|---|',
  '| 作品 | `demo.review` | 审核 | ✅ | ✅ |',
  '| 运维 | `ops.run` | 运维 | — | ✅ |',
  '',
  '```sql',
  "CREATE TABLE gc_queue (reason text NOT NULL CHECK (reason IN ('delete','erase')));",
  '```',
].join('\n');

const CONTRACT_STD = [
  '## §EN-5 审计动作',
  '',
  '```sql',
  'INSERT INTO audit_actions (key, description) VALUES',
  "    ('create', '新建'), ('erase', '擦除');",
  '```',
  '',
  '**2 条**，且这是唯一权威。',
  '',
  '## §EN-6 审计实体类型',
  '',
  '```sql',
  'INSERT INTO audit_entity_types (key, description, table_name, route_template) VALUES',
  "    ('demo', '作品', 'demos', '/admin/demos/{id}');",
  '```',
  '',
  '**1 条**。',
  '',
  '## §EN-7 GC 对象类型',
  '',
  '```sql',
  'INSERT INTO gc_kinds (key, description) VALUES',
  "    ('demo_dir', '作品目录');",
  '```',
  '',
  '**1 条**。',
  '',
  '## §EN-8 GC 原因',
  '',
  '取值域：`delete` / `erase`',
].join('\n');

const CONTRACT_LIFECYCLE = [
  '```yaml',
  'version: 1',
  'tables:',
  '  - table: demos',
  '    entity_type: demo',
  '    derivatives:',
  '      - {kind: demo_dir, from: slug}',
  '```',
].join('\n');

const contractDocs = () => ({
  governance: CONTRACT_GOV,
  standard: CONTRACT_STD,
  lifecycle: CONTRACT_LIFECYCLE,
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


test('契约门禁：权限必须绑全 admin、计数要对、退役键与反例豁免', () => {
  assert.deepEqual(inspectContracts(contractDocs()), []);

  const missing = contractDocs();
  missing.governance = missing.governance.replace('| 运维 | `ops.run` | 运维 | — | ✅ |', '| 运维 | `ops.run` | 运维 | — | — |');
  assert.ok(inspectContracts(missing).some((p) => p.includes('admin')));

  const offByOne = contractDocs();
  offByOne.governance = offByOne.governance.replace('（2 个）', '（3 个）');
  assert.ok(inspectContracts(offByOne).some((p) => p.includes('实际')));

  const stale = contractDocs();
  stale.governance += '\n调它需要 `demo.erase`。\n';
  assert.ok(inspectContracts(stale).some((p) => p.includes('demo.erase')));

  const negative = contractDocs();
  negative.governance += '\n**反例**：写成 `demo.erase` 会破坏命名。\n';
  assert.deepEqual(inspectContracts(negative), []);
});

test('契约门禁：注册表里的 kind / entity_type 必须在目录表里', () => {
  const badKind = contractDocs();
  badKind.lifecycle = badKind.lifecycle.replace('kind: demo_dir', 'kind: oss_object');
  assert.ok(inspectContracts(badKind).some((p) => p.includes('oss_object')));

  const badEntity = contractDocs();
  badEntity.lifecycle = badEntity.lifecycle.replace('entity_type: demo', 'entity_type: account');
  assert.ok(inspectContracts(badEntity).some((p) => p.includes('account')));

  const duplicated = contractDocs();
  duplicated.standard = duplicated.standard.replace("('create', '新建')", "('create', '新建'), ('create', '重复')");
  assert.ok(inspectContracts(duplicated).some((p) => p.includes('重复')));
});

test('契约门禁：gc_queue.reason 的 CHECK 必须与 §EN-8 取值域一致', () => {
  const mismatch = contractDocs();
  mismatch.governance = mismatch.governance.replace("('delete','erase')", "('delete','erase','purge')");
  assert.ok(inspectContracts(mismatch).some((p) => p.includes('gc_queue.reason')));
});
