// 作品形态文档门禁的自测：纯函数，不碰真仓库。
import assert from 'node:assert/strict';
import { test } from 'node:test';

import { checkShapeDocs, parseCode, parseDoc } from './check-shape-docs.mjs';

const CODE = `
var singleFileKinds = map[string]string{
	".html": "html",
	".glb":  "glb",
}
const zipEntry = "index.html"
// .pck 说明 zip 里可能有 Godot 包
`;

const DOC = `
| 类型 | 收哪些格式 | 入口 |
| --- | --- | --- |
| web（网页作品） | \`single:html\`、\`zip:html\` | index.html |
| godot（Godot 游戏） | \`zip:godot\` | index.html |
| model（三维模型） | \`single:glb\` | index.glb |
| link（外链） | \`url\` | 外部地址 |
`;

function migrations(pairs) {
  const map = new Map();
  for (const [type, format] of pairs) {
    if (!map.has(type)) map.set(type, new Set());
    map.get(type).add(format);
  }
  return map;
}

test('代码里认识的格式从 shapes.go 读出来', () => {
  assert.deepEqual([...parseCode(CODE)].sort(), ['single:glb', 'single:html', 'url', 'zip:godot', 'zip:html']);
});

test('文档表格按类型收集格式', () => {
  const { rows } = parseDoc(DOC);
  assert.deepEqual([...rows.get('web')].sort(), ['single:html', 'zip:html']);
  assert.deepEqual([...rows.get('model')], ['single:glb']);
});

test('代码、数据、文档三方一致时通过', () => {
  const { ok, problems } = checkShapeDocs({
    code: parseCode(CODE),
    migrations: migrations([
      ['web', 'single:html'],
      ['web', 'zip:html'],
      ['model', 'single:glb'],
    ]),
    doc: DOC,
  });
  assert.equal(ok, true, problems.join('；'));
});

test('代码支持但文档没写的格式要报出来', () => {
  const doc = DOC.replace('、`zip:html`', '');
  const { ok, problems } = checkShapeDocs({
    code: parseCode(CODE),
    migrations: migrations([['web', 'single:html']]),
    doc,
  });
  assert.equal(ok, false);
  assert.ok(
    problems.some((p) => p.includes('zip:html') && p.includes('没写到')),
    problems.join('；'),
  );
});

test('文档写了代码不认识的格式要报出来', () => {
  const doc = DOC.replace('`single:glb`', '`single:glb`、`single:obj`');
  const { ok, problems } = checkShapeDocs({
    code: parseCode(CODE),
    migrations: migrations([['model', 'single:glb']]),
    doc,
  });
  assert.equal(ok, false);
  assert.ok(
    problems.some((p) => p.includes('single:obj') && p.includes('不认识')),
    problems.join('；'),
  );
});

test('数据库某个类型收了某格式、文档那一行没写要报出来', () => {
  const { ok, problems } = checkShapeDocs({
    code: parseCode(CODE),
    migrations: migrations([
      ['web', 'single:html'],
      ['web', 'zip:html'],
      ['model', 'single:glb'],
      ['zip', 'zip:html'],
    ]),
    doc: DOC,
  });
  assert.equal(ok, false);
  assert.ok(
    problems.some((p) => p.includes('`zip`') && p.includes('zip:html')),
    problems.join('；'),
  );
});
