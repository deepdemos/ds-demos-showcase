#!/usr/bin/env node
// PR 门禁：必须关联 issue、标题类型单一、修改规模不超限。
if (!process.env.GITHUB_EVENT_PATH) {
  console.log('pr-guard skipped: GITHUB_EVENT_PATH is not set (local run)');
  process.exit(0);
}

import fs from 'node:fs';

const MAX_CHANGED_LINES = 500;
const MAX_CHANGED_FILES = 20;
const TITLE_PATTERN = /^(feat|fix|refactor|docs|chore|test|perf|build|ci)(\([a-z0-9.-]+\))?: \S.+/;
const ISSUE_PATTERN = /(?:^|\n)\s*(?:close[sd]?|fix(?:e[sd])?|resolv(?:e[sd])?|refs?|issue)\s*[:：]?\s*#\d+\b/i;
const DOC = 'docs/development-workflow.md';

const event = JSON.parse(fs.readFileSync(process.env.GITHUB_EVENT_PATH, 'utf8'));
const pr = event.pull_request;
if (!pr) {
  console.log('pr-guard skipped: not a pull_request event');
  process.exit(0);
}

const errors = [];
const title = pr.title ?? '';
const body = pr.body ?? '';
const changedLines = (pr.additions ?? 0) + (pr.deletions ?? 0);
const changedFiles = pr.changed_files ?? 0;

if (!TITLE_PATTERN.test(title)) {
  errors.push('PR 标题必须是 Conventional Commits（单一类型），例如 "feat(scope): 描述"。');
}
if (!ISSUE_PATTERN.test(body)) {
  errors.push('PR 必须关联 issue：在描述中使用 "Closes #123"、"Fixes #123" 或 "Refs #123"。');
}
if (changedLines > MAX_CHANGED_LINES) {
  errors.push(`PR 修改 ${changedLines} 行，超过 ${MAX_CHANGED_LINES} 行上限，请拆分。`);
}
if (changedFiles > MAX_CHANGED_FILES) {
  errors.push(`PR 修改 ${changedFiles} 个文件，超过 ${MAX_CHANGED_FILES} 个上限，请拆分。`);
}

if (errors.length > 0) {
  for (const error of errors) console.log(`::error::${error}`);
  console.error(`pr-guard failed (${errors.length}). 处理指引：先读 ${DOC}。`);
  process.exit(1);
}

console.log(
  `pr-guard ok: "${title}" · ${changedLines} lines / ${changedFiles} files · issue linked`,
);
