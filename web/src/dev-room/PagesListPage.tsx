import { Link, useNavigate } from 'react-router';
import { Button } from '@/components/ui';
import { Page } from '@/components/layout/page';
import { pages, type PageEntry } from '@/app/routes';
import { fixturePages } from '@/dev-room/fixtures';

const GROUP_LABEL: Record<PageEntry['group'], string> = {
  public: '公开',
  me: '个人',
  admin: '管理',
  auth: '认证',
  system: '系统',
};

const allPages: readonly PageEntry[] = [...pages, ...fixturePages];
const groups: PageEntry['group'][] = ['public', 'me', 'admin', 'auth', 'system'];

const DEV_TOOLS = [
  { title: '组件开发室', path: '/dev-room', description: '控件库登记与预览' },
  { title: '页面清单', path: '/dev-room/pages', description: '当前页' },
] as const;

function canLink(page: PageEntry): boolean {
  return page.path !== '*' && !page.path.includes(':');
}

export function PagesListPage() {
  const navigate = useNavigate();
  return (
    <Page
      width="wide"
      title="页面清单"
      description={
        'DEV-only：' +
        (allPages.length + DEV_TOOLS.length) +
        ' 条页面（ready + fixture + DEV 工具），按区列出。'
      }
      actions={<Button onClick={() => navigate('/dev-room')}>回组件开发室</Button>}
    >
      {groups.map((group) => {
        const items = allPages.filter((page) => page.group === group);
        if (items.length === 0) return null;
        return (
          <section key={group} className="mt-8">
            <h2 className="text-lg font-semibold">
              {GROUP_LABEL[group]}（{items.length}）
            </h2>
            <table className="mt-3 w-full border-collapse text-sm">
              <thead>
                <tr className="border-b border-gray-200 text-left text-gray-500">
                  <th className="py-2 font-normal">页面</th>
                  <th className="py-2 font-normal">路径</th>
                  <th className="py-2 font-normal">状态</th>
                  <th className="py-2 font-normal">宽度</th>
                </tr>
              </thead>
              <tbody>
                {items.map((page) => (
                  <tr key={page.id} className="border-b border-gray-100">
                    <td className="py-2">
                      {canLink(page) ? (
                        <Link className="underline" to={page.path}>
                          {page.title}
                        </Link>
                      ) : (
                        page.title
                      )}
                    </td>
                    <td className="py-2 font-mono text-xs text-gray-600">{page.path}</td>
                    <td className="py-2 text-xs">
                      {page.availability === 'ready' ? 'ready' : 'DEV fixture'}
                    </td>
                    <td className="py-2 text-xs">{page.width}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </section>
        );
      })}

      <section className="mt-8">
        <h2 className="text-lg font-semibold">DEV 工具（{DEV_TOOLS.length}）</h2>
        <table className="mt-3 w-full border-collapse text-sm">
          <thead>
            <tr className="border-b border-gray-200 text-left text-gray-500">
              <th className="py-2 font-normal">页面</th>
              <th className="py-2 font-normal">路径</th>
              <th className="py-2 font-normal">说明</th>
            </tr>
          </thead>
          <tbody>
            {DEV_TOOLS.map((tool) => (
              <tr key={tool.path} className="border-b border-gray-100">
                <td className="py-2">
                  <Link className="underline" to={tool.path}>
                    {tool.title}
                  </Link>
                </td>
                <td className="py-2 font-mono text-xs text-gray-600">{tool.path}</td>
                <td className="py-2 text-xs">{tool.description}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </section>
    </Page>
  );
}
