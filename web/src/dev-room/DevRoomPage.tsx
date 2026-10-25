import { controls } from '@/components/ui/registry';
import styles from './DevRoomPage.module.css';

export function DevRoomPage() {
  return (
    <section>
      <header className={styles.header}>
        <h1>Dev Room</h1>
        <p>控件库登记页。每个控件都必须在这里出现；用控件 ID 指代具体控件。</p>
      </header>
      <div className={styles.grid}>
        {controls.map(({ id, name, description, Demo }) => (
          <article key={id} className={styles.card} data-control-id={id}>
            <div className={styles.titleRow}>
              <h2>{name}</h2>
              <code className={styles.id}>#{id}</code>
            </div>
            <p>{description}</p>
            <div className={styles.demo}>
              <Demo />
            </div>
          </article>
        ))}
      </div>
    </section>
  );
}
