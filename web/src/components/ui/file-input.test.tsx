import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, test, vi } from 'vitest';
import { FileInput } from './file-input';

function pick(input: HTMLInputElement, file: File) {
  Object.defineProperty(input, 'files', { value: [file], configurable: true });
  fireEvent.change(input);
}

function nativeInput(container: HTMLElement): HTMLInputElement {
  const el = container.querySelector('input[type="file"]');
  if (!el) throw new Error('没有渲染 file input');
  return el as HTMLInputElement;
}

describe('FileInput', () => {
  test('合法文件直接交给调用方', () => {
    const onFileChange = vi.fn();
    const onReject = vi.fn();
    const { container } = render(
      <FileInput onFileChange={onFileChange} onReject={onReject} maxBytes={1000} />,
    );

    const file = new File(['x'], 'a.zip');
    pick(nativeInput(container), file);

    expect(onFileChange).toHaveBeenCalledWith(file);
    expect(onReject).not.toHaveBeenCalled();
  });

  test('超限时不交给调用方，并给出可读原因', () => {
    const onFileChange = vi.fn();
    const onReject = vi.fn();
    const { container } = render(
      <FileInput onFileChange={onFileChange} onReject={onReject} maxBytes={10} />,
    );

    pick(nativeInput(container), new File(['x'.repeat(20)], 'big.zip'));

    // 关键：调用方拿到的是 null 而不是那个超限文件——否则提交时还得再判一次，
    // 而每一处都判一次就意味着总有一处忘了判。
    expect(onFileChange).toHaveBeenCalledWith(null);
    expect(onReject).toHaveBeenCalledTimes(1);
    expect(String(onReject.mock.calls[0]?.[0])).toContain('超过上限');
  });

  test('选中后显示文件名与大小', () => {
    const { container, rerender } = render(<FileInput onFileChange={() => {}} />);
    rerender(
      <FileInput onFileChange={() => {}} file={new File(['abc'], 'demo.zip')} label="作品包" />,
    );
    expect(screen.getByText(/demo\.zip/)).toBeInTheDocument();
    expect(container).toBeTruthy();
  });

  test('错误文案通过 role=alert 暴露给读屏软件', () => {
    render(<FileInput onFileChange={() => {}} error="文件超过上限 200 MB" />);
    expect(screen.getByRole('alert')).toHaveTextContent('文件超过上限 200 MB');
  });
});
