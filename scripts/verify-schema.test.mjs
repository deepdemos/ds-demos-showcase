import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { hasSql, stripComments, statements } from './sql-split.mjs';

test('hasSql: 纯注释块不算语句', () => {
  assert.equal(hasSql('-- 只有注释\n-- 两行'), false);
  assert.equal(hasSql('  -- 缩进注释\n\n'), false);
  assert.equal(hasSql('CREATE TABLE a (id int)'), true);
});

test('stripComments: 去掉行尾注释，保留正文', () => {
  assert.equal(stripComments('a int  -- 主键'), 'a int  ');
  assert.equal(stripComments('-- 整行\nb int'), '\nb int');
});

test('statements: 多条语句按「分号+换行」切开，每条形如 ...;', () => {
  const got = statements('CREATE TABLE a (id int);\nCREATE TABLE b (id int);\n');
  assert.deepEqual(got, ['CREATE TABLE a (id int);', 'CREATE TABLE b (id int);']);
});

test('statements: 纯注释块被丢弃，不产生空语句', () => {
  const sql = ['-- ===== 一张表 =====', '-- 说明两句', 'CREATE TABLE a (id int);', '', '-- 尾巴', ''].join(
    '\n',
  );
  assert.deepEqual(statements(sql), ['CREATE TABLE a (id int);']);
});

test('statements: 以分号结尾的注释不会变成假切点', () => {
  const sql = ['CREATE TABLE a (', '  id int  -- 这是 id;', ');', ''].join('\n');
  const got = statements(sql);
  assert.equal(got.length, 1);
  assert.match(got[0], /CREATE TABLE a \(\s*id int\s*\);/);
});

test('statements: 末尾没有换行的最后一条也能切出来，且只有一个分号', () => {
  assert.deepEqual(statements('CREATE TABLE a (id int);'), ['CREATE TABLE a (id int);']);
  assert.deepEqual(statements('CREATE TABLE a (id int);\n'), ['CREATE TABLE a (id int);']);
});

test('statements: 空文件返回空数组', () => {
  assert.deepEqual(statements(''), []);
  assert.deepEqual(statements('\n\n'), []);
});

test('statements: 真实 schema 文件全部能切完，且没有残留空块', () => {
  const dir = path.join(process.cwd(), 'db', 'schema');
  for (const f of fs.readdirSync(dir).filter((x) => x.endsWith('.sql'))) {
    const got = statements(fs.readFileSync(path.join(dir, f), 'utf8'));
    assert.ok(got.length > 0, f + ' 切出 0 条语句');
    for (const s of got) {
      assert.ok(s.trim().endsWith(';'), f + ' 有一条语句没以分号结尾');
      assert.ok(hasSql(s), f + ' 切出了纯注释块');
    }
  }
});
