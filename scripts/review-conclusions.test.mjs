import assert from 'node:assert/strict';
import { test } from 'node:test';

import { checkConclusions, isTestFile } from './review-conclusions.mjs';

/**
 * 结论落地门禁的自测。
 *
 * 这些用例钉的不是「格式好看」，而是「丢一条能不能发现」——
 * 四轮 review 约 50 条发现全部「已修」却一条没变成门禁，就是这里没约束。
 */
const opts = {
  findTestFn: (name) => name === 'TestGetDemoHiddenChannelIs404' || name === '404 时展示作品不存在',
  hasLoosePointFn: (n) => n === 30 || n === 27,
};

function body(lines, declared) {
  const head = '## 结论落地\n';
  const count = declared === undefined ? '' : `共 ${declared} 条。\n`;
  return `先随便说点背景。\n\n${head}${count}${lines.join('\n')}\n`;
}

test('四条齐全（test/gate/loose/wontfix）：通过', () => {
  const { ok, problems, landed } = checkConclusions(
    body(
      [
        '- F1 详情没判渠道 → test: TestGetDemoHiddenChannelIs404',
        '- F2 判定写了十二遍 → gate: scripts/check-single-source.mjs',
        '- F3 历史版本下载 → loose: 30',
        '- F4 扩展名可伪造 → wontfix: 走魔数嗅探，伪造扩展名改不了解析结果',
      ],
      4,
    ),
    opts,
  );
  assert.deepEqual(problems, []);
  assert.equal(ok, true);
  assert.equal(landed, 4);
});

test('没有「结论落地」段落：红', () => {
  const { ok, problems } = checkConclusions('## 核对\n- F1 有问题\n', opts);
  assert.equal(ok, false);
  assert.ok(problems.some((p) => p.includes('缺少「## 结论落地」段落')));
});

test('声明 5 条只落地 4 条：红（有发现被丢了）', () => {
  const { problems } = checkConclusions(
    body(
      [
        '- F1 详情没判渠道 → test: TestGetDemoHiddenChannelIs404',
        '- F2 判定写了十二遍 → gate: scripts/check-single-source.mjs',
        '- F3 历史版本下载 → loose: 30',
        '- F4 扩展名可伪造 → wontfix: 走魔数嗅探，伪造扩展名改不了解析结果',
      ],
      5,
    ),
    opts,
  );
  assert.ok(problems.some((p) => p.includes('声明共 5 条，实际落地 4 条')));
});

test('没有「共 N 条」：红（无法判断有没有丢）', () => {
  const { problems } = checkConclusions(body(['- F1 详情没判渠道 → test: TestGetDemoHiddenChannelIs404']), opts);
  assert.ok(problems.some((p) => p.includes('缺少「共 N 条」')));
});

test('指向不存在的测试：红', () => {
  const { problems } = checkConclusions(body(['- F1 详情没判渠道 → test: TestNotExist']), opts);
  assert.ok(problems.some((p) => p.includes('指向的测试「TestNotExist」不存在')));
});

test('指向不存在的门禁脚本：红', () => {
  const { problems } = checkConclusions(body(['- F1 判定重复 → gate: scripts/nope.mjs']), opts);
  assert.ok(problems.some((p) => p.includes('门禁脚本「scripts/nope.mjs」不存在')));
});

test('松点编号不存在：红', () => {
  const { problems } = checkConclusions(body(['- F1 某件事 → loose: 99']), opts);
  assert.ok(problems.some((p) => p.includes('没有这一条')));
});

test('不修但理由太短：红', () => {
  const { problems } = checkConclusions(body(['- F1 某件事 → wontfix: 不修']), opts);
  assert.ok(problems.some((p) => p.includes('理由太短')));
});

test('摘要太短：红（下一个看不懂说的是什么）', () => {
  const { problems } = checkConclusions(body(['- F1 啊 → test: TestGetDemoHiddenChannelIs404']), opts);
  assert.ok(problems.some((p) => p.includes('摘要太短')));
});

test('写了段落但一条都没有：红', () => {
  const { problems } = checkConclusions(body(['随便写点东西'], 0), opts);
  assert.ok(problems.some((p) => p.includes('一条都没有')));
});

test('「已修」不算收口：只有这一句就红', () => {
  const bodyText = '## 结论落地\n共 1 条。\n- F1 全部已修。\n';
  const { problems } = checkConclusions(bodyText, opts);
  assert.ok(problems.some((p) => p.includes('一条都没有')));
});

test('前端用例名也能认', () => {
  const { problems } = checkConclusions(body(['- F1 404 文案 → test: 404 时展示作品不存在'], 1), opts);
  assert.deepEqual(problems, []);
});

test('冒号与箭头的中英文写法都认', () => {
  const { problems } = checkConclusions(
    body(['- F1 中文冒号 → test：TestGetDemoHiddenChannelIs404', '- F2 英文箭头 -> loose: 27'], 2),
    opts,
  );
  assert.deepEqual(problems, []);
});

test('遇到下一个二级标题就停止（段落边界）', () => {
  const text = '## 结论落地\n共 1 条。\n- F1 详情没判渠道 → test: TestGetDemoHiddenChannelIs404\n\n## 别的段落\n- F9 这不是结论 → test: TestNotExist\n';
  const { problems, landed } = checkConclusions(text, opts);
  assert.deepEqual(problems, []);
  assert.equal(landed, 1);
});

test('e2e 用例文件算测试文件：标题可以当结论的落点', () => {
  // 少认这一类，门禁就会把「这条能力有 e2e 钉着」判成「测试不存在」。
  assert.equal(isTestFile('lifecycle.e2e.ts'), true);
});

test('门禁自己的用例也算：判据有门禁用例钉着是最该被认的证据', () => {
  // 第六轮的实际形状：F3 的落点是 check-api-doc.test.mjs 里那条用例，
  // 而扫描根里没有 scripts/、扩展名过滤也不认 .mjs —— 「有门禁用例钉着」
  // 反过来过不了门禁。
  assert.equal(isTestFile('check-api-doc.test.mjs'), true);
  assert.equal(isTestFile('check-api-doc.mjs'), false);
});

test('普通源码文件不算测试文件：随便一个函数名糊不过去', () => {
  assert.equal(isTestFile('lifecycle.ts'), false);
  assert.equal(isTestFile('handlers.go'), false);
});
