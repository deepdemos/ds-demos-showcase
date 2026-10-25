import { Checkbox } from './checkbox';

export function CheckboxDefaultDemo() {
  return <Checkbox label="公开作品" defaultChecked indeterminate={false} />;
}

export function CheckboxDisabledDemo() {
  return <Checkbox label="禁用状态" disabled indeterminate={false} />;
}
