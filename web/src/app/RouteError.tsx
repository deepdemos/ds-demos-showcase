import { isRouteErrorResponse, Link, useRouteError } from 'react-router';
import { Button } from '@/components/ui';

/**
 * 路由级兜底页。
 *
 * # 为什么要有
 *
 * 没有 errorElement 时，路由里任何一处抛错（渲染期异常、被拒的请求在渲染里重抛）
 * 都会让整棵树直接卸载：屏幕上只剩浏览器给的白底黑字，既没有导航也没有出路。
 * 用户唯一能做的是清地址栏——而我们本来就知道该给他哪三条路。
 *
 * # 为什么把原始错误也印出来
 *
 * 「页面出错了」这句话对用户没有信息量，对来报问题的人也没有。真正有用的是
 * `HTTP 500`、`boom` 这类原文：用户截图发过来，看一眼就知道是接口挂了还是前端崩了。
 * 这正是异常页该做的事，不是普通页面该干的事。
 */
function describeError(error: unknown): string {
  if (isRouteErrorResponse(error)) {
    return `${error.status} ${error.statusText}`.trim();
  }
  if (error instanceof Error) return error.message;
  if (typeof error === 'string') return error;
  return '';
}

export function RouteError() {
  const error = useRouteError();
  const detail = describeError(error);

  return (
    <section className="mx-auto w-full max-w-md px-4 py-16">
      <h1 className="text-2xl font-semibold">页面出错了</h1>
      <p className="mt-2 text-sm text-gray-600 dark:text-gray-400">
        这一步没有走通。可以先重新加载试一次；一直不行的话，回首页再进来。
      </p>

      {detail && (
        <p
          className="mt-4 rounded-lg border border-red-300 bg-red-50 p-3 font-mono text-xs break-all text-red-800 dark:border-red-800 dark:bg-red-950 dark:text-red-200"
          role="alert"
        >
          {detail}
        </p>
      )}

      <div className="mt-6 flex items-center gap-3">
        <Button type="button" primary onClick={() => window.location.reload()}>
          重新加载
        </Button>
        <Link to="/" className="text-sm text-blue-600 dark:text-blue-400">
          回首页
        </Link>
      </div>
    </section>
  );
}
