import { Outlet, useNavigate } from 'react-router';
import { Button } from '@/components/ui';
import styles from './AppLayout.module.css';

export function AppLayout() {
  const navigate = useNavigate();

  return (
    <div className={styles.shell}>
      <header className={styles.header}>
        <span className={styles.brand}>DeepDemos Showcase</span>
        {import.meta.env.DEV && <Button onClick={() => navigate('/dev-room')}>Dev Room</Button>}
      </header>
      <main className={styles.main}>
        <Outlet />
      </main>
    </div>
  );
}
