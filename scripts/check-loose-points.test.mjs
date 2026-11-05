import assert from 'node:assert/strict';
import { test } from 'node:test';

import { checkLoosePoints } from './check-loose-points.mjs';

/**
 * 松点台账门禁的自测。
 *
 * 每条用例都给一段**很小的假台账**，这样能精确验「哪种写法该红」，
 * 而不依赖真台账当时正好有几条。
 */

const head = '## 1. 第一条松点\n';
const ok = `${head}
| | |
| --- | --- |
| 在哪 | 某处 |
| 能保证 | 这件事 |

## 2. 第二条松点

| | |
| --- | --- |
| 在哪 | 别处 |

## 变更记录

| 日期 | 变更 |
| --- | --- |
| 2026-11-05 | 新增第二条 |
| 2026-11-04 | 新增第一条 |
`;

test('正常台账：编号连续、日期行在变更记录里、日期不倒序', () => {
  const r = checkLoosePoints(ok);
  assert.equal(r.ok, true, r.problems.join('; '));
  assert.equal(r.entries, 2);
  assert.equal(r.changes, 2);
});

test('变更记录被插进某条松点的表格里 → 红', () => {
  const broken = ok.replace('| 能保证 | 这件事 |', '| 能保证 | 这件事 |\n| 2026-11-05 | 作品生命周期落地 |');
  const r = checkLoosePoints(broken);
  assert.equal(r.ok, false);
  assert.match(r.problems[0], /出现在「1\. 第一条松点」小节里/);
});

test('编号跳号 → 红（loose: N 的引用会指错）', () => {
  const broken = ok.replace('## 2. 第二条松点', '## 3. 第二条松点');
  const r = checkLoosePoints(broken);
  assert.equal(r.ok, false);
  assert.match(r.problems[0], /编号是 3/);
});

test('编号重号 → 红', () => {
  const broken = ok.replace('## 2. 第二条松点', '## 1. 另一条');
  const r = checkLoosePoints(broken);
  assert.equal(r.ok, false);
  assert.match(r.problems[0], /编号必须从 1 连续/);
});

test('变更记录乱序（新的插在中间）→ 红', () => {
  const broken = ok.replace(
    '| 2026-11-05 | 新增第二条 |\n| 2026-11-04 | 新增第一条 |',
    '| 2026-11-04 | 新增第一条 |\n| 2026-11-05 | 新增第二条 |',
  );
  const r = checkLoosePoints(broken);
  assert.equal(r.ok, false);
  assert.match(r.problems[0], /比上一条/);
});

test('引用式的日期（不是行首单元格）不算变更记录，不误报', () => {
  const withQuote = ok.replace(
    '| 在哪 | 某处 |',
    '| 在哪 | 某处（2026-11-05 那天发现的，见变更记录） |',
  );
  const r = checkLoosePoints(withQuote);
  assert.equal(r.ok, true, r.problems.join('; '));
});

test('空文档不炸：没有小节就是没有可验的东西', () => {
  const r = checkLoosePoints('');
  assert.equal(r.ok, true);
  assert.equal(r.entries, 0);
});
