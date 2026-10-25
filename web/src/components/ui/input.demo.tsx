import { Input } from './input';

export function InputDefaultDemo() {
  return <Input placeholder="请输入内容" />;
}

export function InputErrorDemo() {
  return <Input defaultValue="错误内容" error="示例错误信息" />;
}

export function InputDisabledDemo() {
  return <Input defaultValue="禁用状态" disabled />;
}
