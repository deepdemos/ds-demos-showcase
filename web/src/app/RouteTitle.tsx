import { useEffect } from 'react';
import { matchPath, useLocation } from 'react-router';
import { fixturePages } from '@/dev-room/fixtures';
import { pages, type PageEntry } from './routes';

// fixture 只在 DEV 注册；生产环境连标题查找也不应该命中它们。
const knownPages = import.meta.env.DEV ? [...pages, ...fixturePages] : pages;

const SITE = 'DeepDemos 展示';

/** 按地址找页面；匹配不到时回 404 条目。 */
export function pageForPath(pathname: string): PageEntry {
  const found = knownPages.find(
    (page) => page.path !== '*' && matchPath({ path: page.path, end: true }, pathname) !== null,
  );
  if (found) return found;
  if (!import.meta.env.DEV) {
    const fixture = fixturePages.find(
      (page) => page.path !== '*' && matchPath({ path: page.path, end: true }, pathname) !== null,
    );
    if (fixture) {
      return {
        ...fixture,
        title: '功能尚未接入',
        description: '这个页面只在开发环境提供，正式站还没有对应的服务端接口。',
      };
    }
  }
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
