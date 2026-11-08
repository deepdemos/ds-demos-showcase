import { expect, test } from 'vitest';
import { readReturnTo, returnToLabel, returnToParam } from './return-to';

// 这一组用例守的是一个安全边界和一个易失细节：
// 1) return_to 会直接进 navigate，只允许站内绝对路径；
// 2) 没有落脚点时不要在地址栏留下 `?return_to=%2F` 这种空回执。

test.each([
  [null, '/'],
  [undefined, '/'],
  ['', '/'],
  ['/submit', '/submit'],
  ['/demos?q=x', '/demos?q=x'],
  ['https://evil.example/x', '/'],
  ['//evil.example/x', '/'],
  ['//', '/'],
  ['submit', '/'],
])('readReturnTo(%s) → %s', (raw, expected) => {
  expect(readReturnTo(raw)).toBe(expected);
});

test('带落脚点时拼成查询串', () => {
  expect(returnToParam('/submit')).toBe('?return_to=%2Fsubmit');
  expect(returnToParam('/demos?q=中文')).toBe('?return_to=%2Fdemos%3Fq%3D%E4%B8%AD%E6%96%87');
});

test('接在已有查询串后面时用 &', () => {
  expect(returnToParam('/submit', '&')).toBe('&return_to=%2Fsubmit');
});

test('没有落脚点时不拼参数', () => {
  expect(returnToParam(null)).toBe('');
  expect(returnToParam('https://evil.example')).toBe('');
});

test('常去的落脚点给人话名字，其余原样显示路径', () => {
  expect(returnToLabel('/submit')).toBe('投稿页');
  expect(returnToLabel('/demos/abc-123/edit')).toBe('这个作品的编辑页');
  expect(returnToLabel('/demos')).toBe('作品库');
  expect(returnToLabel('/some/deep/path')).toBe('/some/deep/path');
});
