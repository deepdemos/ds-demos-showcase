import { Link, Navigate } from 'react-router';
import { EmptyState, Skeleton } from '@/components/ui';
import { useAuth } from '@/features/auth/AuthContext';

/** 管理区落地页（#433）：把收件箱、审核、模型导入收在一个入口。 */
export function AdminPage() {
  const { me, isLoading } = useAuth();

  if (isLoading) return <Skeleton lines={3} />;
  if (me === null) return <Navigate to="/login?return_to=/admin" replace />;
  if (me.role !== 'admin' && me.role !== 'moderator') {
    return (
      <section className="mx-auto w-full max-w-3xl px-4 py-8">
        <EmptyState title="没有管理权限" description="这个入口只对管理员与审核员开放。" />
      </section>
    );
  }

  return (
    <section className="mx-auto w-full max-w-3xl px-4 py-8">
      <h1 className="text-2xl font-semibold text-gray-900 dark:text-gray-100">管理</h1>
      <p className="mt-1 text-sm text-gray-500 dark:text-gray-400">
        待办在收件箱；作品审核与发布、模型目录导入各自有独立页面。
      </p>

      <ul className="mt-4 list-none space-y-3 p-0">
        <li>
          <Link
            to="/admin/inbox"
            className="block rounded-lg border border-gray-200 p-4 text-inherit no-underline dark:border-gray-800"
          >
            <p className="font-medium">收件箱</p>
            <p className="text-sm text-gray-500 dark:text-gray-400">
              待审作品、已通过待发布、待处理提案
            </p>
          </Link>
        </li>
        <li>
          <Link
            to="/admin/reviews"
            className="block rounded-lg border border-gray-200 p-4 text-inherit no-underline dark:border-gray-800"
          >
            <p className="font-medium">作品审核</p>
            <p className="text-sm text-gray-500 dark:text-gray-400">裁决与发布到渠道</p>
          </Link>
        </li>
        <li>
          <Link
            to="/admin/models"
            className="block rounded-lg border border-gray-200 p-4 text-inherit no-underline dark:border-gray-800"
          >
            <p className="font-medium">模型目录</p>
            <p className="text-sm text-gray-500 dark:text-gray-400">导入与同步模型信息</p>
          </Link>
        </li>
      </ul>
    </section>
  );
}
