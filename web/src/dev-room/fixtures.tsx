import { useState, type ReactElement } from 'react';
import { Link, useParams } from 'react-router';
import { Button, Checkbox, Input, Textarea } from '@/components/ui';
import { Page } from '@/components/layout/page';
import type { PageEntry } from '@/app/routes';

const NOTE = 'DEV fixture：结构与交互是真的，数据是本地示例。';

function ProfileSettingsFixture() {
  const [displayName, setDisplayName] = useState('示例作者');
  const [saved, setSaved] = useState(false);
  return (
    <Page width="form" title="资料设置" description={NOTE}>
      <form
        className="mt-6 flex flex-col gap-4"
        onSubmit={(event) => {
          event.preventDefault();
          setSaved(true);
        }}
      >
        <label className="flex flex-col gap-1 text-sm">
          显示名
          <Input
            fullWidth
            value={displayName}
            onChange={(event) => setDisplayName(event.target.value)}
          />
        </label>
        <label className="flex flex-col gap-1 text-sm">
          简介
          <Textarea rows={4} maxLength={500} placeholder="本地示例，不会保存到服务端。" />
        </label>
        <Button type="submit" primary>
          保存
        </Button>
        {saved ? (
          <p className="text-sm text-green-700" role="status">
            已保存（本地示例，未写服务端）。
          </p>
        ) : null}
      </form>
    </Page>
  );
}

function PreferencesFixture() {
  const [email, setEmail] = useState(true);
  const [saved, setSaved] = useState(false);
  return (
    <Page width="form" title="偏好设置" description={NOTE}>
      <form
        className="mt-6 flex flex-col gap-4"
        onSubmit={(event) => {
          event.preventDefault();
          setSaved(true);
        }}
      >
        <Checkbox
          label="接收审核结果邮件"
          checked={email}
          indeterminate={false}
          onChange={(event) => setEmail(event.target.checked)}
        />
        <Button type="submit" primary>
          保存偏好
        </Button>
        {saved ? (
          <p className="text-sm text-green-700" role="status">
            偏好已保存（本地示例）。
          </p>
        ) : null}
      </form>
    </Page>
  );
}

function AdminUsersFixture() {
  return (
    <Page width="wide" title="用户" description={NOTE}>
      <table className="mt-6 w-full border-collapse text-sm">
        <thead>
          <tr className="border-b border-gray-200 text-left text-gray-500">
            <th className="py-2 font-normal">账号</th>
            <th className="py-2 font-normal">角色</th>
            <th className="py-2 font-normal">状态</th>
          </tr>
        </thead>
        <tbody>
          {[
            ['alice', 'user', 'active'],
            ['mod', 'moderator', 'active'],
            ['admin', 'admin', 'active'],
          ].map((row) => (
            <tr key={row[0]} className="border-b border-gray-100">
              {row.map((cell, index) => (
                <td key={row[0] + '-' + index} className="py-2">
                  {cell}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </Page>
  );
}

function AdminAuditFixture() {
  return (
    <Page width="wide" title="审计日志" description={NOTE}>
      <ul className="mt-6 list-none divide-y divide-gray-200 border-y border-gray-200 p-0">
        {[1, 2, 3].map((id) => (
          <li key={id} className="py-3">
            <Link className="underline" to={'/admin/audit/' + id}>
              事件 #{id}
            </Link>
            <p className="m-0 mt-1 text-sm text-gray-500">示例操作 · 管理员 · 2026-01-0{id}</p>
          </li>
        ))}
      </ul>
    </Page>
  );
}

function AdminAuditDetailFixture() {
  const { id = '' } = useParams();
  return (
    <Page width="form" title={'审计事件 #' + id} description={NOTE}>
      <pre className="mt-6 overflow-x-auto rounded border border-gray-200 bg-gray-50 p-3 text-xs">
        {JSON.stringify({ id, actor: 'admin', action: 'demo.review', target: 'demo-a' }, null, 2)}
      </pre>
      <p className="mt-4 text-sm">
        <Link className="underline" to="/admin/audit">
          回审计列表
        </Link>
      </p>
    </Page>
  );
}

function AdminSettingsFixture() {
  const [saved, setSaved] = useState(false);
  return (
    <Page width="form" title="站点参数" description={NOTE}>
      <form
        className="mt-6 flex flex-col gap-4"
        onSubmit={(event) => {
          event.preventDefault();
          setSaved(true);
        }}
      >
        <label className="flex flex-col gap-1 text-sm">
          站点名称
          <Input fullWidth defaultValue="DeepDemos 展示" />
        </label>
        <label className="flex flex-col gap-1 text-sm">
          每页条数
          <Input fullWidth type="number" defaultValue={20} />
        </label>
        <Button type="submit" primary>
          保存参数
        </Button>
        {saved ? (
          <p className="text-sm text-green-700" role="status">
            已保存（本地示例，未写服务端）。
          </p>
        ) : null}
      </form>
    </Page>
  );
}

function AdminClassificationsFixture() {
  return (
    <Page width="wide" title="分类词表" description={NOTE}>
      <table className="mt-6 w-full border-collapse text-sm">
        <thead>
          <tr className="border-b border-gray-200 text-left text-gray-500">
            <th className="py-2 font-normal">维度</th>
            <th className="py-2 font-normal">取值</th>
            <th className="py-2 font-normal">作品数</th>
          </tr>
        </thead>
        <tbody>
          {[
            ['形态', '网页', '18'],
            ['技术', 'WebGL', '7'],
            ['主题', '自然', '5'],
          ].map((row) => (
            <tr key={row[0] + row[1]} className="border-b border-gray-100">
              {row.map((cell, index) => (
                <td key={row[0] + '-' + index} className="py-2">
                  {cell}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </Page>
  );
}

function AdminAnnouncementsFixture() {
  return (
    <Page width="wide" title="公告" description={NOTE}>
      <ul className="mt-6 list-none divide-y divide-gray-200 border-y border-gray-200 p-0">
        {['站点上线公告', '审核规则更新', '维护通知'].map((title) => (
          <li key={title} className="py-3 text-sm">
            {title}
            <span className="ml-2 text-gray-500">草稿</span>
          </li>
        ))}
      </ul>
    </Page>
  );
}

function AdminFeaturedFixture() {
  return (
    <Page width="wide" title="精选" description={NOTE}>
      <ul className="mt-6 list-none divide-y divide-gray-200 border-y border-gray-200 p-0">
        {['钢琴卷帘', 'SDF 光线步进', '路径追踪器'].map((title) => (
          <li key={title} className="py-3 text-sm">
            {title}
            <span className="ml-2 text-gray-500">候选</span>
          </li>
        ))}
      </ul>
    </Page>
  );
}

export const fixturePages: readonly PageEntry[] = [
  {
    id: 'fixture-settings-profile',
    path: '/me/settings/profile',
    title: '资料设置',
    description: NOTE,
    group: 'me',
    access: { kind: 'auth' },
    width: 'form',
    availability: 'fixture',
  },
  {
    id: 'fixture-settings-preferences',
    path: '/me/settings/preferences',
    title: '偏好设置',
    description: NOTE,
    group: 'me',
    access: { kind: 'auth' },
    width: 'form',
    availability: 'fixture',
  },
  {
    id: 'fixture-admin-users',
    path: '/admin/users',
    title: '用户',
    description: NOTE,
    group: 'admin',
    access: { kind: 'auth' },
    width: 'wide',
    availability: 'fixture',
  },
  {
    id: 'fixture-admin-audit',
    path: '/admin/audit',
    title: '审计日志',
    description: NOTE,
    group: 'admin',
    access: { kind: 'auth' },
    width: 'wide',
    availability: 'fixture',
  },
  {
    id: 'fixture-admin-audit-detail',
    path: '/admin/audit/:id',
    title: '审计事件',
    description: NOTE,
    group: 'admin',
    access: { kind: 'auth' },
    width: 'form',
    availability: 'fixture',
  },
  {
    id: 'fixture-admin-settings',
    path: '/admin/settings',
    title: '站点参数',
    description: NOTE,
    group: 'admin',
    access: { kind: 'auth' },
    width: 'form',
    availability: 'fixture',
  },
  {
    id: 'fixture-admin-classifications',
    path: '/admin/classifications',
    title: '分类词表',
    description: NOTE,
    group: 'admin',
    access: { kind: 'auth' },
    width: 'wide',
    availability: 'fixture',
  },
  {
    id: 'fixture-admin-announcements',
    path: '/admin/announcements',
    title: '公告',
    description: NOTE,
    group: 'admin',
    access: { kind: 'auth' },
    width: 'wide',
    availability: 'fixture',
  },
  {
    id: 'fixture-admin-featured',
    path: '/admin/featured',
    title: '精选',
    description: NOTE,
    group: 'admin',
    access: { kind: 'auth' },
    width: 'wide',
    availability: 'fixture',
  },
];

export const fixtureElements: Record<string, ReactElement> = {
  'fixture-settings-profile': <ProfileSettingsFixture />,
  'fixture-settings-preferences': <PreferencesFixture />,
  'fixture-admin-users': <AdminUsersFixture />,
  'fixture-admin-audit': <AdminAuditFixture />,
  'fixture-admin-audit-detail': <AdminAuditDetailFixture />,
  'fixture-admin-settings': <AdminSettingsFixture />,
  'fixture-admin-classifications': <AdminClassificationsFixture />,
  'fixture-admin-announcements': <AdminAnnouncementsFixture />,
  'fixture-admin-featured': <AdminFeaturedFixture />,
};
