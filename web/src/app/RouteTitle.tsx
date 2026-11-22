import { useEffect } from 'react';
import { matchPath, useLocation } from 'react-router';
import { pages, type PageEntry } from './routes';

const SITE = 'DeepDemos 展示';

/** 按地址找页面；匹配不到时回 404 条目。 */
export function pageForPath(pathname: string): PageEntry {
  const found = pages.find(
    (page) => page.path !== '*' && matchPath({ path: page.path, end: true }, pathname) !== null,
  );
  if (found) return found;
  const notFound = pages.find((page) => page.path === '*');
  if (!notFound) throw new Error('页面清单缺少 404 路由');
  return notFound;
}

function setMetaDescription(description: string | undefined) {
  let meta = document.querySelector('meta[name="description"]');
  if (!meta) {
    meta = document.createElement('meta');
    meta.setAttribute('name', 'description');
    document.head.appendChild(meta);
  }
  meta.setAttribute('content', description ?? '');
}

/**
 * 浏览器标签页标题与 description。
 *
 * 页面自己的 h1 由 Page 渲染；这里只管浏览器外面看得到的那一层。
 * 详情页拿到实体名后可再覆盖一次（后续批次）。
 */
export function RouteTitle() {
  const { pathname } = useLocation();
  useEffect(() => {
    const page = pageForPath(pathname);
    document.title = page.title + ' · ' + SITE;
    setMetaDescription(page.description);
  }, [pathname]);
  return null;
}
