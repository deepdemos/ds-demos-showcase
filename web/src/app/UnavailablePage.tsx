import { Link } from 'react-router';
import { Page } from '@/components/layout/page';

/** 生产访问 DEV-only fixture 时的出口：不假装成功，也不混成普通 404。 */
export function UnavailablePage() {
  return (
    <Page
      width="narrow"
      title="功能尚未接入"
      description="这个页面只在开发环境提供，正式站还没有对应的服务端接口。"
    >
      <p className="mt-6 flex flex-wrap gap-3">
        <Link className="underline" to="/demos">
          回作品库
        </Link>
        <Link className="underline" to="/">
          回首页
        </Link>
      </p>
    </Page>
  );
}
