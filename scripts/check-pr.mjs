#!/usr/bin/env node
// PR 门禁：必须关联 issue、标题类型单一、改动文件数不超限；字段缺失时 fail-closed。
import fs from 'node:fs';
import { pathToFileURL } from 'node:url';

// 只限文件数，不限行数：行数是「可 review 性」的间接代理，被生成物证明过不可靠。
// 「单个文件多大」由 scripts/check-file-size.mjs 管（>500 警告，>800 失败）。
export const MAX_CHANGED_FILES = 20;
export const DOCS_PREFIX = 'docs/';
// 生成物不计入文件数：改一次契约必然重生成 Go 与 TS 两份产物，谁写都一样。
// 它们不靠人眼 review——CI 会重新生成并 `git diff --exit-code`，不一致直接红。
// 名单只放「整目录都是机器产物」的地方；往这里加路径 = 声明该目录不再手写。
export const GENERATED_PREFIXES = [
  'backend/internal/api/', // oapi-codegen：契约 → Go
  'backend/internal/store/', // sqlc：查询 → Go
  'web/src/api/gen/', // orval：契约 → TypeScript
];
const TITLE_PATTERN = /^(feat|fix|refactor|docs|chore|test|perf|build|ci)(\([A-Za-z0-9.-]+\))?!?: \S.+/;
const ISSUE_PATTERN = /\b(?:close[sd]?|fix(?:e[sd])?|resolv(?:e[sd])?|refs?|issue)\s*[:：]?\s*#[1-9]\d*\b/i;
const DOC = 'docs/development-workflow.md';

/** 全部改动都在 docs/ 下才算纯文档 PR；空列表或含越界段一律按「不是纯文档」处理（fail-closed）。 */
export function isDocsOnly(files) {
  return (
    Array.isArray(files) &&
    files.length > 0 &&
    files.every(
      (file) =>
        typeof file === 'string' &&
        file.startsWith(DOCS_PREFIX) &&
        !file.split('/').includes('..'),
    )
  );
}

export function isGeneratedFile(path) {
  return typeof path === 'string' && GENERATED_PREFIXES.some((prefix) => path.startsWith(prefix));
}

/** 把一条改动记录统一成路径字符串（清单里带不带行数都接受）。 */
function toPath(entry) {
  if (typeof entry === 'string') return entry;
  if (entry && typeof entry.path === 'string') return entry.path;
  return null;
}

/** 只数「人写的」文件：生成物不进预算。 */
export function handwrittenFiles(files) {
  const paths = (Array.isArray(files) ? files : []).map(toPath).filter(Boolean);
  const hand = paths.filter((path) => !isGeneratedFile(path));
  return {
    files: hand.length,
    skipped: paths.filter((path) => isGeneratedFile(path)),
  };
}

export function validatePullRequest(pr) {
  const errors = [];
  const paths = (Array.isArray(pr.files) ? pr.files : []).map(toPath).filter(Boolean);
  const title = typeof pr.title === 'string' ? pr.title : '';
  const rawBody = typeof pr.body === 'string' ? pr.body : '';
  const body = rawBody.replace(/```[\s\S]*?```/g, '').replace(/^ {4}.*$/gm, '');

  if (!TITLE_PATTERN.test(title)) {
    errors.push('PR 标题必须是 Conventional Commits（单一类型），例如 "feat(scope): 描述"。');
  }
  if (!ISSUE_PATTERN.test(body)) {
    errors.push('PR 必须关联 issue：在描述中使用 "Closes #123"、"Fixes #123" 或 "Refs #123"。');
  }

  if (isDocsOnly(paths)) {
    return errors;
  }

  // 拿不到改动清单时退回 payload 的 changed_files（含生成物），宁可多算也不放行。
  const changedFiles =
    paths.length > 0 ? handwrittenFiles(paths).files : undefined;
  let counted = changedFiles;
  if (counted === undefined) {
    if (typeof pr.changed_files !== 'number' || !Number.isFinite(pr.changed_files)) {
      errors.push('PR payload 缺少 changed_files，且改动清单为空，无法校验规模，门禁失败。');
      return errors;
    }
    counted = pr.changed_files;
  }

  if (counted > MAX_CHANGED_FILES) {
    errors.push(`PR 手写部分改动 ${counted} 个文件，超过 ${MAX_CHANGED_FILES} 个文件上限，请拆分。`);
  }
  return errors;
}

/** 从 changed-files 清单读取：每行可能是 "path"，也可能是 "path|additions|deletions"。 */
export function parseChangedFiles(text) {
  return String(text)
    .split('\n')
    .map((line) => line.trim())
    .filter(Boolean)
    .map((line) => line.split('|')[0]);
}

function readChangedFiles() {
  const file = process.env.CHANGED_FILES_PATH ?? 'changed-files.txt';
  try {
    return parseChangedFiles(fs.readFileSync(file, 'utf8'));
  } catch {
    return [];
  }
}

function main() {
  if (!process.env.GITHUB_EVENT_PATH) {
    console.log('pr-guard skipped: GITHUB_EVENT_PATH is not set (local run)');
    return;
  }
  const event = JSON.parse(fs.readFileSync(process.env.GITHUB_EVENT_PATH, 'utf8'));
  const pr = event.pull_request;
  if (!pr) {
    console.log('pr-guard skipped: not a pull_request event');
    return;
  }

  const files = readChangedFiles();
  const errors = validatePullRequest({ ...pr, files });
  if (errors.length > 0) {
    for (const error of errors) console.log(`::error::${error}`);
    console.error(`pr-guard failed (${errors.length}). 处理指引：先读 ${DOC}。`);
    process.exit(1);
  }

  const size = handwrittenFiles(files);
  const scope = isDocsOnly(files) ? 'docs-only（豁免规模上限）' : 'code';
  const skipped = size.skipped.length > 0 ? ` · 生成物 ${size.skipped.length} 个不计入` : '';
  const lines = pr.additions + pr.deletions;
  console.log(
    `pr-guard ok: "${pr.title}" · ${scope} · ${size.files} files${skipped} · ${lines} lines（仅记录，不限） · issue linked`,
  );
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  main();
}
