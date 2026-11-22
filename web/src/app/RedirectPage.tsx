import { generatePath, Navigate, useLocation, useParams } from 'react-router';

/**
 * 旧地址跳转，保留查询串。
 *
 * React Router 的 <Navigate to="/new"> 默认丢掉 search；而 '?state=deleted'
 * 这类筛选正是旧链接里会带的东西。参数（:id）在这里替换，地址栏不留旧路径。
 */
export function RedirectPage({ to }: { to: string }) {
  const { search, hash } = useLocation();
  const params = useParams();
  const pathname = generatePath(to, params);
  return <Navigate to={{ pathname, search, hash }} replace />;
}
