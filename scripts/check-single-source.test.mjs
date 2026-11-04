import { test } from 'node:test';
import assert from 'node:assert/strict';
import { RULES, compare, applyUpdate, scan } from './check-single-source.mjs';

// 这道门禁自己必须被验：它的全部价值在于「新增副本就红」，
// 如果它悄悄放过了副本，那它比没有更糟（会让人以为收敛过了）。

const rule = (over) => ({
  id: 'demo',
  title: '示例规则',
  pattern: /FORBIDDEN/g,
  include: [/\.sql$/],
  allowed: [],
  hint: '改法',
  ...over,
});

test('规则表：每条都要有 id / 标题 / 正则 / 扫什么 / 改法', () => {
  assert.ok(RULES.length >= 4, '四条核心判定都该有规则');
  for (const r of RULES) {
    assert.match(r.id, /^[a-z][a-z-]*$/);
    assert.ok(r.title && r.title.length > 0, `${r.id} 缺标题`);
    assert.ok(r.pattern instanceof RegExp, `${r.id} 的 pattern 不是正则`);
    assert.ok(Array.isArray(r.include) && r.include.length > 0, `${r.id} 没声明 include`);
    assert.ok(r.hint && r.hint.length > 10, `${r.id} 的 hint 太短，红了之后没人知道怎么改`);
  }
});

test('首次立账之后再出现同类判定：判为 emerged', () => {
  const current = { demo: { 'db/queries/a.sql': 1, 'db/queries/b.sql': 1 } };
  const baseline = { demo: { 'db/queries/a.sql': 1 } };
  const { errors } = compare(current, baseline, [rule()]);
  assert.equal(errors.length, 1);
  assert.equal(errors[0].kind, 'emerged');
  assert.equal(errors[0].file, 'db/queries/b.sql');
});

test('同一文件里再多写一遍：判为 growth 并带出基线数', () => {
  const current = { demo: { 'db/queries/a.sql': 3 } };
  const baseline = { demo: { 'db/queries/a.sql': 1 } };
  const { errors } = compare(current, baseline, [rule()]);
  assert.equal(errors[0].kind, 'growth');
  assert.equal(errors[0].count, 3);
  assert.equal(errors[0].was, 1);
});

test('清理了副本却没收紧基线：判为 stale（棘轮必须往下拨）', () => {
  const current = { demo: { 'db/queries/a.sql': 1 } };
  const baseline = { demo: { 'db/queries/a.sql': 3 } };
  const { errors } = compare(current, baseline, [rule()]);
  assert.equal(errors[0].kind, 'stale');
});

test('整条文件清理干净：也算 stale，不能悄悄变宽松', () => {
  const current = {};
  const baseline = { demo: { 'db/queries/a.sql': 2 } };
  const { errors } = compare(current, baseline, [rule()]);
  assert.equal(errors.length, 1);
  assert.equal(errors[0].kind, 'stale');
  assert.equal(errors[0].count, 0);
});

test('与基线完全一致：不报错', () => {
  const current = { demo: { 'db/queries/a.sql': 2 } };
  const baseline = { demo: { 'db/queries/a.sql': 2 } };
  const { errors } = compare(current, baseline, [rule()]);
  assert.deepEqual(errors, []);
});

test('--update 不允许把大盘变大（否则就是「一键变绿」后门）', () => {
  const before = { demo: { 'a.sql': 1 } };
  const after = { demo: { 'a.sql': 1, 'b.sql': 2 } };
  assert.throws(() => applyUpdate(after, before), /拒绝更新/);
});

test('--update 允许变小，并且换文件但总量不变也允许', () => {
  const before = { demo: { 'a.sql': 3 } };
  assert.deepEqual(applyUpdate({ demo: { 'a.sql': 1 } }, before), { demo: { 'a.sql': 1 } });
  assert.deepEqual(applyUpdate({ demo: { 'b.sql': 1 } }, before), { demo: { 'b.sql': 1 } });
});

test('白名单里的文件不计副本（那是唯一出处）', () => {
  const files = ['db/queries/allowed.sql'];
  const hits = scan(files, [rule({ allowed: ['db/queries/allowed.sql'] })]);
  assert.deepEqual(hits, {});
});

test('exclude 生效：测试文件与库结构不算副本', () => {
  const files = ['backend/internal/httpapi/handlers_test.go', 'db/schema/30-demo.sql'];
  const hits = scan(files, [
    rule({ include: [/\.go$/], exclude: [/_test\.go$/] }),
    rule({ id: 'ddl', include: [/\.sql$/], exclude: [/^db\/schema\//] }),
  ]);
  assert.deepEqual(hits, {});
});

test('当前仓库真的过这道门禁（否则基线就是装饰）', async () => {
  const { execFileSync } = await import('node:child_process');
  const out = execFileSync(process.execPath, ['scripts/check-single-source.mjs'], { encoding: 'utf8' });
  assert.match(out, /单一出处门禁通过/);
});
