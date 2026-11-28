import { describe, expect, test } from 'vitest';

import { isModelDelisted, profileCompleteness } from './profile-completeness';

describe('profileCompleteness', () => {
  test('无档案行（null / undefined）判为 missing', () => {
    expect(profileCompleteness(null)).toBe('missing');
    expect(profileCompleteness(undefined)).toBe('missing');
  });

  test('有档案行但缺 contextWindow 或 modalitiesIn 任一项判为 partial', () => {
    expect(profileCompleteness({})).toBe('partial');
    expect(profileCompleteness({ contextWindow: 128000 })).toBe('partial');
    expect(profileCompleteness({ contextWindow: 128000, modalitiesIn: '   ' })).toBe('partial');
    expect(profileCompleteness({ modalitiesIn: 'text' })).toBe('partial');
  });

  test('contextWindow 与 modalitiesIn 均有值判为 complete', () => {
    expect(profileCompleteness({ contextWindow: 128000, modalitiesIn: 'text,image' })).toBe(
      'complete',
    );
  });
});

describe('isModelDelisted', () => {
  test('已退役且无合并归宿判为已下架', () => {
    expect(isModelDelisted({ status: 'deprecated', mergedIntoSlug: '' })).toBe(true);
  });

  test('已退役但有合并归宿属于已合并，不是已下架', () => {
    expect(isModelDelisted({ status: 'deprecated', mergedIntoSlug: 'deepseek-v3' })).toBe(false);
  });

  test('非退役状态均不是已下架', () => {
    expect(isModelDelisted({ status: 'active', mergedIntoSlug: '' })).toBe(false);
    expect(isModelDelisted({ status: 'candidate', mergedIntoSlug: '' })).toBe(false);
    expect(isModelDelisted({ status: 'unverified', mergedIntoSlug: '' })).toBe(false);
  });
});
