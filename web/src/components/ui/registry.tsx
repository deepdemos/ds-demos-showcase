import type { ReactNode } from 'react';
import { ButtonDemo } from './button.demo';
import { CheckboxDemo } from './checkbox.demo';
import { InputDemo } from './input.demo';

export interface ControlSpec {
  /** 稳定控件 ID：用户与 agent 用它指代控件，不随文案变化。 */
  id: string;
  name: string;
  description: string;
  Demo: () => ReactNode;
}

export const controls: ControlSpec[] = [
  {
    id: 'button',
    name: 'Button',
    description: '按钮：默认、primary、danger、disabled、loading。',
    Demo: ButtonDemo,
  },
  {
    id: 'input',
    name: 'Input',
    description: '输入框：受控、禁用、错误态。',
    Demo: InputDemo,
  },
  {
    id: 'checkbox',
    name: 'Checkbox',
    description: '复选框：受控、禁用。',
    Demo: CheckboxDemo,
  },
];
