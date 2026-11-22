import { Link } from 'react-router';
import { Page } from '@/components/layout/page';

// 404 页曾经只有「404」和「页面不存在。」——用户除了手改地址栏没有别的路。
// 这里给两条：回作品库（大多数人的目的地）和回首页（不知道要去哪的人）。
const actionLink =
  'inline-flex items-center rounded-lg border border-gray-300 px-4 py-2 text-sm hover:border-gray-400';

export function NotFoundPage() {
  return (
    <Page
      width="narrow"
      title="404"
      description="页面不存在。可能是地址写错了，也可能这个页面已经搬走。"
    >
      <p className="mt-6 flex flex-wrap gap-3">
        <Link to="/demos" className={actionLink}>
          回作品库
        </Link>
        <Link to="/" className={actionLink}>
          回首页
        </Link>
      </p>
    </Page>
  );
}
