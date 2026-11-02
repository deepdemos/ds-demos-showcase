import type { ReactNode } from 'react';
import {
  ButtonDangerDemo,
  ButtonDefaultDemo,
  ButtonDisabledDemo,
  ButtonPrimaryDemo,
} from './button.demo';
import { CheckboxDefaultDemo, CheckboxDisabledDemo } from './checkbox.demo';
import { EmptyStateDefaultDemo, EmptyStateWithActionDemo } from './empty-state.demo';
import { ErrorStateDefaultDemo, ErrorStateWithRetryDemo } from './error-state.demo';
import { InputDefaultDemo, InputDisabledDemo, InputErrorDemo } from './input.demo';
import { PaginationDefaultDemo, PaginationDisabledDemo } from './pagination.demo';
import { SkeletonDefaultDemo, SkeletonCardDemo } from './skeleton.demo';
import { ThemeToggleDemo } from './theme-toggle.demo';

export const controlCategories = [
  { id: 'action', name: '操作' },
  { id: 'input', name: '输入' },
  { id: 'selection', name: '选择' },
  { id: 'navigation', name: '导航' },
  { id: 'feedback', name: '反馈' },
] as const;

export type ControlCategoryId = (typeof controlCategories)[number]['id'];

export interface ControlSpec {
  /** 注册名：<控件>/<样式>，同一种控件可以注册多种样式。 */
  id: string;
  /** 对应控件库的导出名。 */
  component: string;
  name: string;
  category: ControlCategoryId;
  description: string;
  Demo: () => ReactNode;
}

export const controls: ControlSpec[] = [
  {
    id: 'button/default',
    component: 'Button',
    name: '默认按钮',
    category: 'action',
    description: '最基础的按钮样式。',
    Demo: ButtonDefaultDemo,
  },
  {
    id: 'button/primary',
    component: 'Button',
    name: '主要按钮',
    category: 'action',
    description: '页面主操作使用。',
    Demo: ButtonPrimaryDemo,
  },
  {
    id: 'button/danger',
    component: 'Button',
    name: '危险按钮',
    category: 'action',
    description: '删除等不可逆操作使用。',
    Demo: ButtonDangerDemo,
  },
  {
    id: 'button/disabled',
    component: 'Button',
    name: '禁用按钮',
    category: 'action',
    description: '不可用状态。',
    Demo: ButtonDisabledDemo,
  },
  {
    id: 'input/default',
    component: 'Input',
    name: '默认输入框',
    category: 'input',
    description: '单行文本输入。',
    Demo: InputDefaultDemo,
  },
  {
    id: 'input/error',
    component: 'Input',
    name: '错误输入框',
    category: 'input',
    description: '校验失败时的错误态。',
    Demo: InputErrorDemo,
  },
  {
    id: 'input/disabled',
    component: 'Input',
    name: '禁用输入框',
    category: 'input',
    description: '不可编辑状态。',
    Demo: InputDisabledDemo,
  },
  {
    id: 'checkbox/default',
    component: 'Checkbox',
    name: '默认复选框',
    category: 'selection',
    description: '二选一或多选。',
    Demo: CheckboxDefaultDemo,
  },
  {
    id: 'checkbox/disabled',
    component: 'Checkbox',
    name: '禁用复选框',
    category: 'selection',
    description: '不可操作状态。',
    Demo: CheckboxDisabledDemo,
  },
  {
    id: 'theme-toggle/default',
    component: 'ThemeToggle',
    name: '主题切换',
    category: 'action',
    description: '浅色 / 深色模式切换。',
    Demo: ThemeToggleDemo,
  },
  {
    id: 'pagination/default',
    component: 'Pagination',
    name: '分页器',
    category: 'navigation',
    description: '首 / 上一页 / 当前页 / 下一页 / 末页。页码只在 1..pageCount 之间变化。',
    Demo: PaginationDefaultDemo,
  },
  {
    id: 'pagination/disabled',
    component: 'Pagination',
    name: '禁用分页器',
    category: 'navigation',
    description: '加载中或不可翻页时整组禁用。',
    Demo: PaginationDisabledDemo,
  },
  {
    id: 'empty-state/default',
    component: 'EmptyState',
    name: '空状态',
    category: 'feedback',
    description: '说清「这里为什么是空的」，而不是只写「暂无数据」。',
    Demo: EmptyStateDefaultDemo,
  },
  {
    id: 'empty-state/with-action',
    component: 'EmptyState',
    name: '带操作的空状态',
    category: 'feedback',
    description: '空态下给一个下一步动作，例如清空筛选。',
    Demo: EmptyStateWithActionDemo,
  },
  {
    id: 'skeleton/default',
    component: 'Skeleton',
    name: '骨架屏',
    category: 'feedback',
    description: '首屏未拿到数据时的占位。已有数据刷新时不要用（会闪）。',
    Demo: SkeletonDefaultDemo,
  },
  {
    id: 'skeleton/card',
    component: 'Skeleton',
    name: '卡片骨架',
    category: 'feedback',
    description: '更高的行高，模拟卡片布局。',
    Demo: SkeletonCardDemo,
  },
  {
    id: 'error-state/default',
    component: 'ErrorState',
    name: '错误状态',
    category: 'feedback',
    description: '说清发生了什么，不要贴后端错误原文。',
    Demo: ErrorStateDefaultDemo,
  },
  {
    id: 'error-state/with-retry',
    component: 'ErrorState',
    name: '带重试的错误状态',
    category: 'feedback',
    description: '错误态必须有出路：能重试就给重试。',
    Demo: ErrorStateWithRetryDemo,
  },
];
