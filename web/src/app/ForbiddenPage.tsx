import { Link } from 'react-router';
import { Page } from '@/components/layout/page';

/** 403 不是 404：地址是对的，只是当前账号不能进。 */
export function ForbiddenPage() {
  return (
    <Page width="narrow" title="403" description="当前账号没有进入这个区域的权限。">
      <p className="mt-4 text-sm text-gray-600">
        管理区只对管理员与审核员开放。如果你认为这是误判，请联系站点维护者。
      </p>
      <p className="mt-4 flex flex-wrap gap-3 text-sm">
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
