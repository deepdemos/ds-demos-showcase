import { Link } from 'react-router';
import { Page } from '@/components/layout/page';

export function SettingsPage() {
  return (
    <Page
      width="narrow"
      title="设置"
      description="账号资料与偏好设置；内容管理入口放在收藏夹、浏览历史与通知里。"
    >
      <h2 className="mt-6 text-lg font-semibold">资料与偏好</h2>
      {import.meta.env.DEV ? (
        <p className="mt-2 flex flex-wrap gap-3 text-sm">
          <Link className="underline" to="/me/settings/profile">
            资料设置
          </Link>
          <Link className="underline" to="/me/settings/preferences">
            偏好设置
          </Link>
        </p>
      ) : (
        <p className="mt-2 text-sm text-gray-700">
          资料与偏好设置尚未接入服务端；当前先在收藏夹、浏览历史与通知里管理自己的内容。
        </p>
      )}

      <h2 className="mt-6 text-lg font-semibold">内容管理</h2>
      <p className="mt-2 flex flex-wrap gap-3 text-sm">
        <Link className="underline" to="/me/collections">
          收藏夹
        </Link>
        <Link className="underline" to="/me/history">
          浏览历史
        </Link>
        <Link className="underline" to="/me/notifications">
          通知
        </Link>
      </p>
    </Page>
  );
}
