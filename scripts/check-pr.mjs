#!/usr/bin/env node
// PR 门禁：必须关联 issue、标题类型单一、修改规模不超限；字段缺失时 fail-closed。
import fs from 'node:fs';
import { pathToFileURL } from 'node:url';

export const MAX_CHANGED_LINES = 500;
export const MAX_CHANGED_FILES = 20;
const TITLE_PATTERN = /^(feat|fix|refactor|docs|chore|test|perf|build|ci)(\([A-Za-z0-9.-]+\))?!?: \S.+/;
const ISSUE_PATTERN = /\b(?:close[sd]?|fix(?:e[sd])?|resolv(?:e[sd])?|refs?|issue)\s*[:：]?\s*#[1-9]\d*\b/i;
const DOC = 'docs/development-workflow.md';

export function validatePullRequest(pr) {
  const errors = [];
  const title = typeof pr.title === 'string' ? pr.title : '';
  const rawBody = typeof pr.body === 'string' ? pr.body : '';
  const body = rawBody.replace(/```[\s\S]*?```/g, '').replace(/^ {4}.*$/gm, '');

  if (!TITLE_PATTERN.test(title)) {
    errors.push('PR 标题必须是 Conventional Commits（单一类型），例如 "feat(scope): 描述"。');
  }
  if (!ISSUE_PATTERN.test(body)) {
    errors.push('PR 必须关联 issue：在描述中使用 "Closes #123"、"Fixes #123" 或 "Refs #123"。');
  }

  const numericFields = ['additions', 'deletions', 'changed_files'];
  const missing = numericFields.filter(
    (field) => typeof pr[field] !== 'number' || !Number.isFinite(pr[field]),
  );
  if (missing.length > 0) {
    errors.push(`PR payload 缺少 ${missing.join(', ')}，无法校验规模，门禁失败。`);
    return errors;
  }

  const changedLines = pr.additions + pr.deletions;
  if (changedLines > MAX_CHANGED_LINES) {
    errors.push(`PR 修改 ${changedLines} 行，超过 ${MAX_CHANGED_LINES} 行上限，请拆分。`);
  }
  if (pr.changed_files > MAX_CHANGED_FILES) {
    errors.push(`PR 修改 ${pr.changed_files} 个文件，超过 ${MAX_CHANGED_FILES} 个文件上限，请拆分。`);
  }
  return errors;
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

  const errors = validatePullRequest(pr);
  if (errors.length > 0) {
    for (const error of errors) console.log(`::error::${error}`);
    console.error(`pr-guard failed (${errors.length}). 处理指引：先读 ${DOC}。`);
    process.exit(1);
  }

  console.log(
    `pr-guard ok: "${pr.title}" · ${pr.additions + pr.deletions} lines / ${pr.changed_files} files · issue linked`,
  );
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  main();
}