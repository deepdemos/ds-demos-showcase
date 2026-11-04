#!/usr/bin/env node
// review 锁：PR 上必须有 review 评论，且 review 必须晚于最新 commit。
// 不确定的问题必须先 ask，不允许在 review 里默认作者假设。
import { pathToFileURL } from 'node:url';

import { checkConclusions } from './review-conclusions.mjs';

export const MIN_REVIEW_BODY = 10;
const REVIEW_STATES = new Set(['COMMENTED', 'APPROVED', 'CHANGES_REQUESTED']);
const DOC = 'docs/development-workflow.md';

/** 取「晚于最新 commit 的那条」review 正文，用来验结论落地。 */
export function latestReview(reviews, headCommitDate) {
  const headTime = Date.parse(headCommitDate ?? '');
  if (!Number.isFinite(headTime) || !Array.isArray(reviews)) return null;
  const eligible = reviews
    .filter((review) => {
      const t = Date.parse(review?.submitted_at ?? '');
      return REVIEW_STATES.has(review?.state) && Number.isFinite(t) && t > headTime;
    })
    .sort((a, b) => Date.parse(a.submitted_at) - Date.parse(b.submitted_at));
  return eligible.length > 0 ? eligible[eligible.length - 1] : null;
}

export function hasReviewComment(reviews, headCommitDate) {
  const headTime = Date.parse(headCommitDate ?? '');
  if (!Number.isFinite(headTime) || !Array.isArray(reviews)) return false;
  return reviews.some((review) => {
    const reviewTime = Date.parse(review?.submitted_at ?? '');
    return (
      REVIEW_STATES.has(review?.state) &&
      typeof review.body === 'string' &&
      review.body.trim().length >= MIN_REVIEW_BODY &&
      Number.isFinite(reviewTime) &&
      reviewTime > headTime
    );
  });
}

async function api(path, token) {
  const response = await fetch(`https://api.github.com/repos/${process.env.GITHUB_REPOSITORY}${path}`, {
    headers: {
      Authorization: `Bearer ${token}`,
      Accept: 'application/vnd.github+json',
      'User-Agent': 'ds-demos-review-guard',
    },
  });
  if (!response.ok) {
    throw new Error(`GitHub API ${path} 返回 ${response.status}`);
  }
  return response.json();
}

async function main() {
  const token = process.env.GH_TOKEN;
  const prNumber = process.env.PR_NUMBER;
  if (!token || !process.env.GITHUB_REPOSITORY || !prNumber) {
    console.error('review-guard failed: GH_TOKEN / GITHUB_REPOSITORY / PR_NUMBER 缺失，门禁 fail-closed。');
    process.exit(1);
  }

  try {
    const pr = await api(`/pulls/${prNumber}`, token);
    const headSha = pr.head?.sha;
    if (!headSha) throw new Error('无法读取 PR head SHA');
    const commit = await api(`/commits/${headSha}`, token);
    const headCommitDate = commit.commit?.committer?.date ?? commit.commit?.author?.date;
    if (!headCommitDate) throw new Error('无法读取最新 commit 时间');

    const reviews = await api(`/pulls/${prNumber}/reviews?per_page=100`, token);
    if (hasReviewComment(reviews, headCommitDate)) {
      // 时间对了还不够：结论必须落地。每条发现变成 test / gate / 松点 / 明确不修之一，
      // 「已修」不算——改完就没了，没人能证明它不会再犯。格式见 scripts/review-conclusions.mjs。
      const latest = latestReview(reviews, headCommitDate);
      const conclusions = checkConclusions(latest?.body ?? '');
      if (!conclusions.ok) {
        for (const problem of conclusions.problems) console.log(`::error::${problem}`);
        console.error(`review-guard failed：PR #${prNumber} 的 review 结论没有落地（${conclusions.problems.length} 条问题）。`);
        console.error('每条发现三选一：变 test/gate、记进 docs/已知松点.md、或写清为什么不修。');
        console.error(`格式见 ${DOC} 的「review 结论落地」一节。`);
        process.exit(1);
      }
      console.log(
        `review-guard ok: PR #${prNumber} 有晚于 ${headCommitDate} 的 review 评论，` +
          `结论落地 ${conclusions.landed} 条${conclusions.declared !== null ? `（声明 ${conclusions.declared}）` : ''}`,
      );
      return;
    }

    const hasAnySubmitted = reviews.some(
      (review) =>
        REVIEW_STATES.has(review?.state) &&
        typeof review.body === 'string' &&
        review.body.trim().length >= MIN_REVIEW_BODY,
    );
    const reason = hasAnySubmitted
      ? '最新 review 早于最新 commit，push 后必须重新 review。'
      : 'PR 上必须有已提交的 review 评论（body >=10 字符）。';
    console.log(`::error::${reason} review 中遇到不确定的问题必须先 ask 用户/负责人，不允许自行假设。`);
    console.error(`review-guard failed（最新 commit: ${headCommitDate}）。处理指引：见 ${DOC}。`);
    process.exit(1);
  } catch (error) {
    console.error(`review-guard failed: ${error instanceof Error ? error.message : String(error)}`);
    console.error(`处理指引：见 ${DOC}；不确定的问题先 ask，不要自行假设。`);
    process.exit(1);
  }
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  main();
}
