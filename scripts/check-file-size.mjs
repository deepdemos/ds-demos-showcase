#!/usr/bin/env node
// 文件行数门禁：>500 行警告（不红），>800 行失败。
// 统计对象 = git 跟踪的源码文件行数（物理行，不含生成/声明文件）。
import { execFileSync } from 'node:child_process';
import fs from 'node:fs';
import path from 'node:path';

const WARN_LINES = 500;
const FAIL_LINES = 800;
const EXTENSIONS = new Set(['.go', '.ts', '.tsx', '.js', '.jsx', '.mjs', '.cjs', '.css']);
const IGNORED = [/.d.ts$/i, /(^|\/)web\/src\/lib\/api\/gen\//];

const files = execFileSync('git', ['ls-files'], { encoding: 'utf8' })
  .split('\n')
  .filter(Boolean)
  .filter((file) => EXTENSIONS.has(path.extname(file).toLowerCase()))
  .filter((file) => !IGNORED.some((pattern) => pattern.test(file)));

const rows = [];
for (const file of files) {
  const content = fs.readFileSync(file, 'utf8').replaceAll('\r\n', '\n');
  const parts = content.split('\n');
  if (parts.at(-1) === '') parts.pop();
  const lines = parts.length;
  if (lines > WARN_LINES) rows.push({ file, lines });
}

rows.sort((a, b) => b.lines - a.lines);
const hardFailures = rows.filter((row) => row.lines > FAIL_LINES);

for (const { file, lines } of rows) {
  const message = `${file} 有 ${lines} 行，超过 ${WARN_LINES} 行，存在过耦合风险；超过 ${FAIL_LINES} 行将失败。`;
  if (lines > FAIL_LINES) {
    console.log(`::error file=${file},line=1::${message} 必须拆分后再提交。`);
  } else {
    console.log(`::warning file=${file},line=1::${message}`);
  }
}

if (process.env.GITHUB_STEP_SUMMARY) {
  const summary = [
    '## 文件行数门禁',
    '',
    `阈值：>${WARN_LINES} 行警告，>${FAIL_LINES} 行失败。检查 ${files.length} 个源码文件。`,
    '',
    ...(rows.length === 0
      ? ['未发现超过阈值的文件。']
      : ['| 文件 | 行数 | 结果 |', '| --- | ---: | --- |',
         ...rows.map(({ file, lines }) => `| \`${file}\` | ${lines} | ${lines > FAIL_LINES ? '失败' : '警告'} |`)]),
    '',
  ].join('\n');
  fs.appendFileSync(process.env.GITHUB_STEP_SUMMARY, summary);
}

if (hardFailures.length > 0) {
  console.error(`file-size failed: ${hardFailures.length} 个文件超过 ${FAIL_LINES} 行，请先拆分。`);
  console.error('处理指引：见 docs/development-workflow.md（先建 issue，再拆分修复，PR 关联 issue）。');
  process.exit(1);
}

console.log(
  `file-size ok: ${files.length} files checked, ${rows.length} warning(s), 0 failure(s)`,
);
