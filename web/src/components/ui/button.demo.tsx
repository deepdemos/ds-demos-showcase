import { Button } from './button';
import styles from './demo.module.css';

export function ButtonDemo() {
  return (
    <div className={styles.row}>
      <Button>Default</Button>
      <Button primary>Primary</Button>
      <Button danger>Danger</Button>
      <Button disabled>Disabled</Button>
      <Button loader>Loading</Button>
    </div>
  );
}
