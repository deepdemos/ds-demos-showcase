import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import test from 'node:test';

import { sortGeneratedIndex } from './sort-gen-index.mjs';

function writeTemp(rel, content) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'sort-gen-'));
  const full = path.join(dir, rel);
  fs.mkdirSync(path.dirname(full), { recursive: true });
  fs.writeFileSync(full, content, 'utf8');
  return { dir, full };
}

const FILE = 'gen/index.ts';

test('乱序的 export 行会被排序', () => {
  const { dir, full } = writeTemp(
    FILE,
    "export * from './身份/身份';\nexport * from './作品/作品';\nexport * from './系统/系统';\n",
  );
  assert.equal(sortGeneratedIndex(FILE, dir), true);
  assert.equal(
    fs.readFileSync(full, 'utf8'),
    "export * from './作品/作品';\nexport * from './系统/系统';\nexport * from './身份/身份';\n",
  );
});

test('已排好序时不动文件（幂等）', () => {
  const { dir, full } = writeTemp(
    FILE,
    "export * from './a';\nexport * from './b';\n",
  );
  const before = fs.statSync(full).mtimeMs;
  assert.equal(sortGeneratedIndex(FILE, dir), false);
  assert.equal(fs.statSync(full).mtimeMs, before);
});

test('非 export 行保持原位，只有 export 行参与重排', () => {
  const { dir, full } = writeTemp(
    FILE,
    "// 头注释\nexport * from './b';\n\n// 中间\nexport * from './a';\n",
  );
  assert.equal(sortGeneratedIndex(FILE, dir), true);
  assert.equal(
    fs.readFileSync(full, 'utf8'),
    "// 头注释\nexport * from './a';\n\n// 中间\nexport * from './b';\n",
  );
});

test('文件不存在时返回 false 而不是抛', () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'sort-gen-'));
  assert.equal(sortGeneratedIndex(FILE, dir), false);
});

test('只有一行 export 时不排序', () => {
  const { dir } = writeTemp(FILE, "export * from './only';\n");
  assert.equal(sortGeneratedIndex(FILE, dir), false);
});
