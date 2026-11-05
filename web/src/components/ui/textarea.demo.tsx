import { Textarea } from './textarea';

export function TextareaDefaultDemo() {
  return <Textarea placeholder="写点什么" aria-label="默认多行输入" />;
}

export function TextareaErrorDemo() {
  return <Textarea defaultValue="太短了" error="至少写 10 个字" aria-label="错误态多行输入" />;
}

export function TextareaDisabledDemo() {
  return <Textarea defaultValue="不可编辑" disabled aria-label="禁用的多行输入" />;
}
