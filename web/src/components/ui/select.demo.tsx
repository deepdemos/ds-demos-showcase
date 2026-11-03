import { useState } from 'react';
import { Select } from './select';

const TYPES = [
  { value: 'web', label: '网页作品' },
  { value: 'zip', label: '压缩包作品' },
  { value: 'link', label: '外部链接' },
];

export function SelectDefaultDemo() {
  const [value, setValue] = useState('web');
  return <Select value={value} options={TYPES} onChange={setValue} ariaLabel="作品类型" />;
}

export function SelectPlaceholderDemo() {
  const [value, setValue] = useState('');
  return (
    <Select
      value={value}
      options={TYPES}
      onChange={setValue}
      ariaLabel="作品类型"
      placeholder="请选择作品类型"
    />
  );
}

export function SelectErrorDemo() {
  const [value, setValue] = useState('');
  return (
    <Select
      value={value}
      options={TYPES}
      onChange={setValue}
      ariaLabel="作品类型"
      placeholder="请选择作品类型"
      error="请选择作品类型"
    />
  );
}

export function SelectDisabledDemo() {
  return <Select value="web" options={TYPES} onChange={() => {}} ariaLabel="作品类型" disabled />;
}
