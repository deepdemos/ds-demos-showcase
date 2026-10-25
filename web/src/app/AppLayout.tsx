import { Outlet, useNavigate } from 'react-router';
import { Button } from '@/components/ui';
import styles from './AppLayout.module.css';

export function AppLayout() {
  const navigate = useNavigate();

  return (
    <div className={styles.shell}>
      <header className={styles.header}>
        <span className={styles.brand}>DeepDemos 展示</span>
        {import.meta.env.DEV && <Button onClick={() => navigate('/dev-room')}>开发房间</Button>}
      </header>
      <main className={styles.main}>
        <Outlet />
      </main>
    </div>
  );
}
