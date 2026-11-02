#!/usr/bin/env node
// PR 门禁：必须关联 issue、标题类型单一、修改规模不超限；字段缺失时 fail-closed。
import fs from 'node:fs';
import { pathToFileURL } from 'node:url';

export const MAX_CHANGED_LINES = 500;
export const MAX_CHANGED_FILES = 20;
// 纯文档 PR 只豁免规模限制，标题与 issue 关联仍然校验：
// 设计文档天然上千行，按 500 行拆会把一份连贯的设计切成碎片。
export const DOCS_PREFIX = 'docs/';
// 生成物豁免规模：改一次契约必然重生成 Go 与 TS 两份产物，谁写都一样。
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

/** 把一条改动记录统一成 {path, additions, deletions}。只给路径时行数为 0。 */
function toEntry(entry) {
  // 只有路径时行数记为 null（未知），不是 0——否则会被当成「真的没改动」而放行。
  if (typeof entry === 'string') return { path: entry, additions: null, deletions: null };
  if (!entry || typeof entry.path !== 'string') return null;
  const num = (v) => (typeof v === 'number' && Number.isFinite(v) && v >= 0 ? v : null);
  return { path: entry.path, additions: num(entry.additions), deletions: num(entry.deletions) };
}

/**
 * 算「人写的」规模：去掉生成物。
 *
 * hasStats 为 false 时（清单只有路径、没有每文件行数）退回 PR payload 的汇总数字——
 * 那是含生成物的口径，宁可多算也不放行（fail-closed）。
 */
export function handwrittenSize(files) {
  const entries = (Array.isArray(files) ? files : []).map(toEntry).filter(Boolean);
  const hasStats = entries.length > 0 && entries.every((e) => e.additions !== null && e.deletions !== null);
  const hand = entries.filter((e) => !isGeneratedFile(e.path));
  return {
    hasStats,
    files: hand.length,
    lines: hasStats ? hand.reduce((sum, e) => sum + e.additions + e.deletions, 0) : 0,
    skipped: entries.filter((e) => isGeneratedFile(e.path)).map((e) => e.path),
  };
}

export function validatePullRequest(pr) {
  const errors = [];
  const files = Array.isArray(pr.files) ? pr.files : [];
  const paths = files.map((f) => (typeof f === 'string' ? f : f && f.path));
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

  const size = handwrittenSize(files);
  let changedLines;
  let changedFiles;
  if (size.hasStats) {
    changedLines = size.lines;
    changedFiles = size.files;
  } else {
    // 清单里没有每文件行数：退回 payload 汇总（含生成物），不清楚就不给豁免。
    const numericFields = ['additions', 'deletions', 'changed_files'];
    const missing = numericFields.filter(
      (field) => typeof pr[field] !== 'number' || !Number.isFinite(pr[field]),
    );
    if (missing.length > 0) {
      errors.push(`PR payload 缺少 ${missing.join(', ')}，无法校验规模，门禁失败。`);
      return errors;
    }
    changedLines = pr.additions + pr.deletions;
    changedFiles = pr.changed_files;
  }

  if (changedLines > MAX_CHANGED_LINES) {
    errors.push(`PR 手写部分修改 ${changedLines} 行，超过 ${MAX_CHANGED_LINES} 行上限，请拆分。`);
  }
  if (changedFiles > MAX_CHANGED_FILES) {
    errors.push(`PR 手写部分修改 ${changedFiles} 个文件，超过 ${MAX_CHANGED_FILES} 个文件上限，请拆分。`);
  }
  return errors;
}

/** 从 changed-files 清单读取：每行 "path" 或 "path<TAB>additions<TAB>deletions"。 */
export function parseChangedFiles(text) {
  return String(text)
    .split('\n')
    .map((line) => line.trim())
    .filter(Boolean)
    .map((line) => {
      const [path, additions, deletions] = line.split('|');
      return { path, additions: Number(additions), deletions: Number(deletions) };
    });
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

  const paths = files.map((f) => f.path);
  const size = handwrittenSize(files);
  const scope = isDocsOnly(paths) ? 'docs-only（豁免规模上限）' : 'code';
  const skipped = size.skipped.length > 0 ? ` · 生成物 ${size.skipped.length} 个不计入` : '';
  console.log(
    `pr-guard ok: "${pr.title}" · ${scope} · ${size.lines} lines / ${size.files} files${skipped} · issue linked`,
  );
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  main();
}
