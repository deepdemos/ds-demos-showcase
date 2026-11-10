import { render, screen } from '@testing-library/react';
import { describe, expect, test, vi } from 'vitest';

import { Rating } from './rating';

describe('Rating', () => {
  test('点第 4 颗星回调 4', () => {
    const onChange = vi.fn();
    render(<Rating label="打分" value={0} onChange={onChange} />);
    screen.getByRole('radio', { name: '打 4 分' }).click();
    expect(onChange).toHaveBeenCalledWith(4);
  });

  test('点已经选中的那颗星表示撤销', () => {
    const onChange = vi.fn();
    render(<Rating label="打分" value={3} onChange={onChange} />);
    // 文案会变：已选中的那颗说「取消评分」而不是「打 3 分」。
    screen.getByRole('radio', { name: /取消评分/ }).click();
    expect(onChange).toHaveBeenCalledWith(0);
  });

  test('读屏能听出选中状态', () => {
    render(<Rating label="打分" value={4} onChange={() => undefined} />);
    expect(screen.getByRole('radio', { checked: true })).toBeTruthy();
  });

  test('禁用时点击不回调', () => {
    const onChange = vi.fn();
    render(<Rating label="打分" value={2} onChange={onChange} disabled />);
    screen.getByRole('radio', { name: '打 5 分' }).click();
    expect(onChange).not.toHaveBeenCalled();
  });

  // 「你还没评分」说的是我自己的票，与页面上的「还没有人评分」（全站聚合）分开（issue #377）。
  test('没打过分时显示「你还没评分」', () => {
    render(<Rating label="打分" value={0} onChange={() => undefined} showValueText />);
    expect(screen.getByText('你还没评分')).toBeTruthy();
  });
});
