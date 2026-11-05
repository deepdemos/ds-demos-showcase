import assert from 'node:assert/strict';
import { test } from 'node:test';

import { checkApiDoc } from './check-api-doc.mjs';

/**
 * 契约描述门禁的自测。
 *
 * 只验「散文里的数字对不对得上机器产物」这一件事，
 * 所以每个用例都只造一句描述和一个端点表。
 */

const surface = (names) => ({
  models: {},
  enums: {},
  endpoints: Object.fromEntries(names.map((n) => [n, {}])),
});

test('数字与端点表一致 → 绿', () => {
  const r = checkApiDoc({
    spec: '当前范围：作品与身份，共 3 个端点。实现全部已接入。',
    surface: surface(['a', 'b', 'c']),
  });
  assert.equal(r.ok, true, r.problems.join('; '));
  assert.equal(r.stated, 3);
  assert.equal(r.actual, 3);
});

test('数字漂了（第二个版本停在上一个数字）→ 红，并说出两个数', () => {
  const r = checkApiDoc({
    spec: '当前范围：共 18 个端点。',
    surface: surface(Array.from({ length: 24 }, (_, i) => `e${i}`)),
  });
  assert.equal(r.ok, false);
  assert.match(r.problems[0], /共 18 个端点.*实际 24 个/);
});

test('描述里根本没写这个数字 → 红（不给读者判据就等于没写）', () => {
  const r = checkApiDoc({ spec: '当前范围：作品与身份。', surface: surface(['a']) });
  assert.equal(r.ok, false);
  assert.match(r.problems[0], /没有「共 N 个端点」/);
});

test('数字两边有空格或换个量词写法的中文也不会误读', () => {
  const r = checkApiDoc({ spec: '共  2  个端点', surface: surface(['a', 'b']) });
  assert.equal(r.ok, true, r.problems.join('; '));
});

test('端点表为空时认 0：不让「一个端点都没有」悄悄通过', () => {
  const r = checkApiDoc({ spec: '共 1 个端点', surface: surface([]) });
  assert.equal(r.ok, false);
  assert.match(r.problems[0], /实际 0 个/);
});
