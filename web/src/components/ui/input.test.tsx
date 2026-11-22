import { render } from '@testing-library/react';
import { Size } from '@jetbrains/ring-ui-built/components/input/input';
import { describe, expect, test } from 'vitest';
import { Input } from './input';

describe('Input', () => {
  test('fullWidth 映射到 Ring UI 的 FULL', () => {
    const { container } = render(<Input fullWidth aria-label="全宽" />);
    expect(container.querySelector('.ring-input-sizeFULL')).not.toBeNull();
  });

  test('显式 size 优先于 fullWidth', () => {
    const { container } = render(<Input fullWidth size={Size.S} aria-label="小号" />);
    expect(container.querySelector('.ring-input-sizeS')).not.toBeNull();
    expect(container.querySelector('.ring-input-sizeFULL')).toBeNull();
  });
});
