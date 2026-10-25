import { useState } from 'react';
import { Checkbox } from './checkbox';
import styles from './demo.module.css';

export function CheckboxDemo() {
  const [checked, setChecked] = useState(true);

  return (
    <div className={styles.stack}>
      <Checkbox
        label="公开作品"
        checked={checked}
        indeterminate={false}
        onChange={(event) => setChecked(event.target.checked)}
      />
      <Checkbox label="禁用状态" disabled indeterminate={false} />
    </div>
  );
}
