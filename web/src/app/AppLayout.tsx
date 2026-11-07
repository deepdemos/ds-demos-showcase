import { Link, Outlet, useNavigate } from 'react-router';
import { Button } from '@/components/ui';
import { useAuth } from '@/features/auth/AuthContext';
import styles from './AppLayout.module.css';

export function AppLayout() {
  const navigate = useNavigate();
  const { me, isLoading, signOut } = useAuth();

  return (
    <div className={styles.shell}>
      <header className={styles.header}>
        <span className={styles.brand}>DeepDemos 展示</span>
        <nav className="flex items-center gap-2 text-sm">
          <Link to="/demos" className="text-gray-600 hover:underline dark:text-gray-300">
            作品库
          </Link>
          {me && (
            <Link to="/submit" className="text-gray-600 hover:underline dark:text-gray-300">
              投稿
            </Link>
          )}
          {/* 会话还没读出来时不渲染任何登录相关按钮：先显示「登录」再换成用户名，
              会让已经登录的人在每次刷新时看到一次闪烁的错误状态。 */}
          {isLoading ? null : me ? (
            <>
              {/* 这只是「入口可见性」的提示，不是权限判定——真正的判定在服务端
                  （`demo.review`）。所以这里用 or 而不是等号：给某个新角色加权限时，
                  最多是入口不显示，不会出现「有权限但被前端挡住」。 */}
              {me.role === 'admin' || me.role === 'moderator' ? (
                <Link to="/admin/reviews" className="text-sm underline">
                  待审
                </Link>
              ) : null}
              {/* 模型导入只有 admin 有 `model.import`。这里同样只是入口可见性，
                  不是权限判定——moderator 点进来会看到「没有导入权限」。 */}
              {me.role === 'admin' ? (
                <Link to="/admin/models" className="text-sm underline">
                  模型
                </Link>
              ) : null}
              <span className="text-gray-500 dark:text-gray-400">
                {me.displayName || me.username}
              </span>
              <Button onClick={() => void signOut().then(() => navigate('/'))}>登出</Button>
            </>
          ) : (
            <Button onClick={() => navigate('/login')}>登录</Button>
          )}
          {import.meta.env.DEV && <Button onClick={() => navigate('/dev-room')}>开发房间</Button>}
        </nav>
      </header>
      <main className={styles.main}>
        <Outlet />
      </main>
    </div>
  );
}
