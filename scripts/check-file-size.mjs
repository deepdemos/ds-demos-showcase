#!/usr/bin/env node
// 文件行数门禁：>500 行警告（不红），>800 行失败。
// 统计对象 = git 跟踪的源码文件物理行数，排除 .d.ts 声明文件与**生成目录**。
//
// 为什么排除生成目录：这道门禁防的是「手写的东西耦合了、该拆」，而
// `backend/internal/store/*.sql.go`、`backend/internal/api/*.gen.go`、`web/src/api/gen/`
// 三处整目录都是机器产物——它们没法「拆」，只能改上游（SQL 查询文件 / 契约）。
// 上游文件本身仍受这道门禁约束，所以「查询写得太长」这类问题不会被放过去。
// 这条口径与 `check-pr.mjs` 的 GENERATED_PREFIXES 共用同一个常量，不各写一份。
import { execFileSync } from 'node:child_process';
import fs from 'node:fs';
import path from 'node:path';
import { pathToFileURL } from 'node:url';

import { isGeneratedFile } from './check-pr.mjs';

export const WARN_LINES = 500;
export const FAIL_LINES = 800;
const EXTENSIONS = new Set(['.go', '.ts', '.tsx', '.js', '.jsx', '.mjs', '.cjs', '.css']);
const IGNORED = [/\.d\.ts$/i];

export function listSourceFiles(cwd = process.cwd()) {
  const output = execFileSync('git', ['-c', 'core.quotePath=false', 'ls-files', '-z'], {
    cwd,
    encoding: 'utf8',
  });
  return output
    .split('\0')
    .filter(Boolean)
    .filter((file) => EXTENSIONS.has(path.extname(file).toLowerCase()))
    .filter((file) => !IGNORED.some((pattern) => pattern.test(file)))
    .filter((file) => !isGeneratedFile(file));
}

export function countTextLines(content) {
  const parts = content.replaceAll('\r\n', '\n').split('\n');
  if (parts.at(-1) === '') parts.pop();
  return parts.length;
}

export function classifyLines(lines) {
  if (lines > FAIL_LINES) return 'error';
  if (lines > WARN_LINES) return 'warning';
  return 'ok';
}

export function collectOversizedFiles(cwd = process.cwd()) {
  return listSourceFiles(cwd)
    // `git ls-files` 会继续列出「已删但尚未 stage」的文件；这里跳过，否则门禁会甩一段
    // ENOENT 堆栈而不是给出结论。删文件还没提交时门禁不该崩。
    .filter((file) => fs.existsSync(path.join(cwd, file)))
    .map((file) => ({
      file,
      lines: countTextLines(fs.readFileSync(path.join(cwd, file), 'utf8')),
    }))
    .filter((row) => row.lines > WARN_LINES)
    .sort((a, b) => b.lines - a.lines);
}

function main() {
  const files = listSourceFiles();
  const rows = collectOversizedFiles();
  const hardFailures = rows.filter((row) => classifyLines(row.lines) === 'error');

  for (const { file, lines } of rows) {
    const message = `${file} 有 ${lines} 行，超过 ${WARN_LINES} 行，存在过耦合风险；超过 ${FAIL_LINES} 行将失败。`;
    if (classifyLines(lines) === 'error') {
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
        : [
            '| 文件 | 行数 | 结果 |',
            '| --- | ---: | --- |',
            ...rows.map(
              ({ file, lines }) =>
                `| \`${file}\` | ${lines} | ${classifyLines(lines) === 'error' ? '失败' : '警告'} |`,
            ),
          ]),
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
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  main();
}
