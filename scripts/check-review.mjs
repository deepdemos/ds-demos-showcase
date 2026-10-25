#!/usr/bin/env node
// review 锁：PR 上必须至少有 1 条已提交的 review 评论，不能只在聊天里 review。
import { pathToFileURL } from 'node:url';

export const MIN_REVIEW_BODY = 10;
const REVIEW_STATES = new Set(['COMMENTED', 'APPROVED', 'CHANGES_REQUESTED']);
const DOC = 'docs/development-workflow.md';

export function hasReviewComment(reviews) {
  return Array.isArray(reviews) && reviews.some(
    (review) =>
      REVIEW_STATES.has(review?.state) &&
      typeof review.body === 'string' &&
      review.body.trim().length >= MIN_REVIEW_BODY,
  );
}

async function main() {
  const token = process.env.GH_TOKEN;
  const repo = process.env.GITHUB_REPOSITORY;
  const prNumber = process.env.PR_NUMBER;
  if (!token || !repo || !prNumber) {
    console.error('review-guard failed: GH_TOKEN / GITHUB_REPOSITORY / PR_NUMBER 缺失，门禁 fail-closed。');
    process.exit(1);
  }

  const response = await fetch(
    `https://api.github.com/repos/${repo}/pulls/${prNumber}/reviews?per_page=100`,
    {
      headers: {
        Authorization: `Bearer ${token}`,
        Accept: 'application/vnd.github+json',
        'User-Agent': 'ds-demos-review-guard',
      },
    },
  );
  if (!response.ok) {
    console.error(`review-guard failed: GitHub API 返回 ${response.status}，无法确认 review 评论。`);
    process.exit(1);
  }

  const reviews = await response.json();
  const valid = reviews.filter(
    (review) =>
      REVIEW_STATES.has(review?.state) &&
      typeof review.body === 'string' &&
      review.body.trim().length >= MIN_REVIEW_BODY,
  );
  if (!hasReviewComment(reviews)) {
    console.log(
      `::error::PR 上必须有至少 1 条 review 评论（body >= ${MIN_REVIEW_BODY} 字符）。用 gh pr review <PR> --comment/--request-changes 回复，issue comment 不算。`,
    );
    console.error(`review-guard failed. 处理指引：见 ${DOC}。`);
    process.exit(1);
  }

  console.log(`review-guard ok: ${valid.length} review(s) on PR #${prNumber}`);
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  main();
}
