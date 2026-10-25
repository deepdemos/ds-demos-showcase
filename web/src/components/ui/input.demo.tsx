import { useState } from 'react';
import { Input } from './input';
import styles from './demo.module.css';

export function InputDemo() {
  const [value, setValue] = useState('DeepDemos');

  return (
    <div className={styles.stack}>
      <Input
        value={value}
        onChange={(event) => setValue(event.target.value)}
        placeholder="Demo 名称"
      />
      <Input defaultValue="禁用状态" disabled />
      <Input defaultValue="错误状态" error="这是一条示例错误" />
    </div>
  );
}
