import { test } from 'node:test';
import assert from 'node:assert/strict';
import { inspectNames, inspectChecksums } from './check-migrations.mjs';

test('命名门禁：连续序号通过', () => {
  assert.deepEqual(inspectNames(['0001_a.sql', '0002_b.sql']), []);
});

test('命名门禁：跳号要报', () => {
  const problems = inspectNames(['0001_a.sql', '0003_c.sql']);
  assert.equal(problems.length, 1);
  assert.match(problems[0], /序号不连续/);
});

test('命名门禁：没从 0001 开始要报', () => {
  const problems = inspectNames(['0002_b.sql']);
  assert.equal(problems.length, 1);
  assert.match(problems[0], /序号不连续/);
});

test('命名门禁：文件名不合规要报', () => {
  for (const bad of ['1_a.sql', '0001-a.sql', '0001_A.sql', '0001.sql']) {
    const problems = inspectNames([bad]);
    assert.equal(problems.length, 1, bad + ' 应当被拒');
    assert.match(problems[0], /文件名必须是 NNNN_名称\.sql/);
  }
});

test('校验和门禁：清单缺失时提示 seal', () => {
  const problems = inspectChecksums(process.cwd(), [], null);
  assert.equal(problems.length, 1);
  assert.match(problems[0], /--seal/);
});

test('校验和门禁：内容变了要报（抓「改已合入的迁移」）', () => {
  const files = [{ path: 'db/migrations/0001_a.sql', sum: 'new' }];
  const problems = inspectChecksums(process.cwd(), files, { 'db/migrations/0001_a.sql': 'old' });
  assert.equal(problems.length, 1);
  assert.match(problems[0], /已合入的迁移不许改/);
});

test('校验和门禁：新文件不在清单里要报', () => {
  const files = [{ path: 'db/migrations/0002_b.sql', sum: 'x' }];
  const problems = inspectChecksums(process.cwd(), files, { 'db/migrations/0001_a.sql': 'x' });
  // 一条是新文件没登记，一条是清单里的文件不见了
  assert.equal(problems.length, 2);
  assert.ok(problems.some((p) => /不在 checksums\.json 里/.test(p)));
  assert.ok(problems.some((p) => /但文件不存在/.test(p)));
});

test('校验和门禁：一致时通过', () => {
  const files = [{ path: 'db/migrations/0001_a.sql', sum: 'abc' }];
  assert.deepEqual(inspectChecksums(process.cwd(), files, { 'db/migrations/0001_a.sql': 'abc' }), []);
});
