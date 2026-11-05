import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, test, vi } from 'vitest';
import { Textarea } from './textarea';

describe('Textarea', () => {
  test('默认是可编辑的多行输入，行数给默认值', () => {
    render(<Textarea aria-label="简介" defaultValue="第一版简介" />);
    const el = screen.getByLabelText('简介');
    expect(el.tagName).toBe('TEXTAREA');
    expect(el).toHaveProperty('rows', 4);
    expect(el).not.toBeDisabled();
  });

  test('输入变化交给调用方', () => {
    const onChange = vi.fn();
    render(<Textarea aria-label="简介" value="" onChange={onChange} />);
    fireEvent.change(screen.getByLabelText('简介'), { target: { value: '新内容' } });
    expect(onChange).toHaveBeenCalledTimes(1);
  });

  test('错误态同时给视觉与语义：红边框 + aria-invalid + 提示可被读屏关联', () => {
    render(<Textarea aria-label="简介" error="至少写 10 个字" />);
    const el = screen.getByLabelText('简介');
    expect(el).toHaveAttribute('aria-invalid', 'true');
    expect(el.className).toContain('border-red-500');
    // 提示文本既是可见的 alert，也要能被 aria-describedby 指到。
    const hint = screen.getByRole('alert');
    expect(hint).toHaveTextContent('至少写 10 个字');
    expect(el.getAttribute('aria-describedby')).toBe(hint.id);
  });

  test('没有错误时不渲染提示，也不加 aria-invalid', () => {
    render(<Textarea aria-label="简介" />);
    const el = screen.getByLabelText('简介');
    expect(el).not.toHaveAttribute('aria-invalid');
    expect(el).not.toHaveAttribute('aria-describedby');
    expect(screen.queryByRole('alert')).toBeNull();
  });

  test('禁用态透传给原生 textarea', () => {
    render(<Textarea aria-label="简介" disabled />);
    expect(screen.getByLabelText('简介')).toBeDisabled();
  });
});
