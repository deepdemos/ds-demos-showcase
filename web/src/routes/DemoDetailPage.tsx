import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useEffect, useState } from 'react';
import { Link, useParams } from 'react-router';
import { EmptyState, ErrorState, Skeleton } from '@/components/ui';
import {
  archiveDemo,
  deleteDemo,
  getDemo,
  postDemoView,
  restoreDemo,
  unarchiveDemo,
  type DemoDetail,
} from '@/api';

type Loaded = { kind: 'ok'; demo: DemoDetail } | { kind: 'missing' };

async function fetchDemo(slug: string): Promise<Loaded> {
  const response = await getDemo(slug);
  if (response.status === 200) return { kind: 'ok', demo: response.data };
  if (response.status === 404) return { kind: 'missing' };
  throw new Error(`作品加载失败（HTTP ${response.status}）`);
}

/**
 * 墓碑文案按「这个作品是怎么消失的」分两种。
 *
 * 标准对象 §ST-2 要求 archived 与 removed 的墓碑内容**必须不同**：一个是作者自己下架的，
 * 一个是平台移除的，混成一句「作品不存在」会让作者以为是被删了。
 *
 * **软删不在其中**：`deleted_at` 非空对外一律 404（生命周期 §4.1「对外就是不存在」），
 * 墓碑只属于「曾经公开过」的两种档位。函数末尾那支兜底文案正常情况下走不到——
 * 走到它说明服务端给的 `lifecycleState` 与这里的假设不一致，宁可显示一句含糊的，
 * 也不要让页面白屏。
 */
export function tombstoneText(demo: Pick<DemoDetail, 'lifecycleState'> & { updatedAt?: string }): {
  title: string;
  description: string;
} {
  const when = demo.updatedAt ? new Date(demo.updatedAt).toLocaleDateString('zh-CN') : '';
  const suffix = when ? `（${when}）` : '';
  if (demo.lifecycleState === 'archived') {
    return { title: '作者已下架这个作品', description: `作品不再公开展示。${suffix}` };
  }
  if (demo.lifecycleState === 'removed') {
    return {
      title: '这个作品已被移除',
      description: `因违反社区规则，作品已从站内移除。${suffix}`,
    };
  }
  return { title: '这个作品已不可见', description: `它当前不再公开展示。${suffix}` };
}

/**
 * 墓碑判定看**状态**，不看标题是否为空。
 *
 * 曾经的写法是「标题为 null 就是墓碑」，那是把「服务端留空某个字段」当成了协议的一部分。
 * 现在墓碑保留标题（`docs/实体域数据结构设计.md` §5.0 的墓碑行列了「slug / 标题 /
 * 下架时间 / 说明」），所以必须按状态判——否则下架的作品会被当成正常作品渲染，
 * 页面里会出现「有标题、无内容」的半截状态。
 */
export function isTombstone(demo: Pick<DemoDetail, 'lifecycleState'>): boolean {
  return demo.lifecycleState === 'archived' || demo.lifecycleState === 'removed';
}

export function DemoDetailPage() {
  const { slug = '' } = useParams();

  const { data, isPending, isError, error, refetch, isFetching } = useQuery({
    queryKey: ['demo', slug],
    queryFn: () => fetchDemo(slug),
    enabled: slug.length > 0,
  });

  // 记一次浏览。
  //
  // 放在这里而不是详情查询里，是因为详情是 GET：给它加副作用，等于让爬虫、
  // 预取、curl 验证全都算一次浏览。这个 effect 只在「作品真的渲染出来」之后跑。
  //
  // 依赖里只放 slug：切到另一件作品才算新的一次；同一件作品重新挂载（比如从编辑页
  // 返回）不该再加一次——去重表那边同一天也只认一次，但少发一次请求总归更好。
  useEffect(() => {
    if (!slug) return;
    void postDemoView(slug).catch(() => {
      // 计数失败与用户无关，页面不该因此报错。
    });
  }, [slug]);

  return (
    <section className="mx-auto w-full max-w-3xl px-4 py-8">
      <p className="mb-4">
        <Link to="/demos" className="text-sm text-gray-500 hover:underline dark:text-gray-400">
          ← 返回作品库
        </Link>
      </p>

      {isPending && <Skeleton lines={4} lineHeight="h-8" />}

      {isError && (
        <ErrorState
          title="作品加载失败"
          description={error instanceof Error ? error.message : undefined}
          onRetry={() => void refetch()}
          retrying={isFetching}
        />
      )}

      {data?.kind === 'missing' && (
        <EmptyState title="作品不存在" description="它可能从未公开发布，或者已经被擦除。" />
      )}

      {data?.kind === 'ok' && isTombstone(data.demo) && (
        <>
          {data.demo.title ? (
            <h1 className="mb-4 text-center text-xl font-semibold text-gray-900 dark:text-gray-100">
              {data.demo.title}
            </h1>
          ) : null}
          <EmptyState {...tombstoneText(data.demo)} />
          <LifecycleBar slug={slug} demo={data.demo} />
        </>
      )}

      {data?.kind === 'ok' && !isTombstone(data.demo) && (
        <article>
          <h1 className="text-2xl font-semibold text-gray-900 dark:text-gray-100">
            {data.demo.title}
          </h1>
          <p className="mt-1 text-xs text-gray-500 dark:text-gray-400">
            发布 {new Date(data.demo.createdAt).toLocaleDateString('zh-CN')}
            {data.demo.updatedAt !== data.demo.createdAt &&
              ` · 更新 ${new Date(data.demo.updatedAt).toLocaleDateString('zh-CN')}`}
          </p>

          {/* 业务统计：服务端只在「已发布且有对外版本」时给这两个数，
              墓碑与未发布一律为 0，所以这里不用再判状态。 */}
          {data.demo.viewCount > 0 || data.demo.downloadCount > 0 ? (
            <p className="mt-1 text-xs text-gray-500 dark:text-gray-400">
              浏览 {data.demo.viewCount.toLocaleString('zh-CN')} 次 · 下载{' '}
              {data.demo.downloadCount.toLocaleString('zh-CN')} 次
            </p>
          ) : null}

          {data.demo.coverUrl ? (
            <img
              src={data.demo.coverUrl}
              alt=""
              className="mt-4 w-full rounded-xl border border-gray-200 object-cover dark:border-gray-800"
            />
          ) : (
            <div className="mt-4 flex h-48 items-center justify-center rounded-xl border border-dashed border-gray-200 bg-gray-50 text-sm text-gray-400 dark:border-gray-800 dark:bg-gray-900 dark:text-gray-500">
              这个作品没有封面
            </div>
          )}

          {data.demo.lifecycleActions.restore ? (
            <p className="mt-4 rounded-lg border border-amber-300 bg-amber-50 px-3 py-2 text-sm text-amber-900 dark:border-amber-800 dark:bg-amber-950 dark:text-amber-200">
              这件作品已删除，恢复期内可以恢复。过了恢复期，就只有你自己能打开这个页面了。
            </p>
          ) : null}

          <p className="mt-4 whitespace-pre-line text-base leading-7 text-gray-700 dark:text-gray-300">
            {data.demo.description ?? '作者没有填写简介。'}
          </p>

          <LifecycleBar slug={slug} demo={data.demo} />

          <div className="mt-4 flex items-center gap-3">
            {data.demo.canEdit ? (
              <Link
                to={`/demos/${encodeURIComponent(slug)}/edit`}
                className="inline-flex items-center rounded-lg border border-gray-300 px-3 py-1.5 text-sm text-gray-700 hover:bg-gray-50 dark:border-gray-700 dark:text-gray-200 dark:hover:bg-gray-800"
              >
                编辑作品
              </Link>
            ) : null}
          </div>

          {data.demo.downloadable ? <DemoDownload slug={slug} /> : null}

          {data.demo.lifecycleState === 'published' ? <DemoPreview slug={slug} /> : null}
        </article>
      )}
    </section>
  );
}

/**
 * 作品预览。
 *
 * # 两道隔离，缺一不可
 *
 * 第一道是服务端给的 `Content-Security-Policy: sandbox …`（见 `preview.go`），
 * 它对「直接在新标签页打开」也生效——那正是 iframe 属性管不到的场景。
 *
 * 第二道就是这里的 `sandbox` 属性。**绝不能加 `allow-same-origin`**：
 * 预览与主站同源，加上它等于把沙箱拆了——脚本能读主站的 DOM、localStorage
 * 和 cookie，一句 `fetch` 就能带着用户身份去调审核接口。
 *
 * 也不加 `allow-top-navigation`：那让作品能把整个标签页导航走。
 */
export function DemoPreview({ slug }: { slug: string }) {
  const src = `/preview/${encodeURIComponent(slug)}/`;
  return (
    <section className="mt-6 space-y-2">
      <div className="flex items-center justify-between">
        <h2 className="text-lg font-medium text-gray-900 dark:text-gray-100">预览</h2>
        <Link to={src} target="_blank" rel="noreferrer" className="text-sm hover:underline">
          新标签页打开
        </Link>
      </div>
      <iframe
        src={src}
        title="作品预览"
        className="h-[70vh] w-full rounded-xl border border-gray-200 bg-white dark:border-gray-800"
        sandbox="allow-scripts allow-forms allow-popups allow-modals allow-pointer-lock"
        referrerPolicy="no-referrer"
      />
    </section>
  );
}

/**
 * 下载入口。
 *
 * # 为什么用 downloadable 而不是自己按 demoType 判断
 *
 * 「能不能下」不是「类型是什么」的函数：外链作品没有本站文件、墓碑态不承载内容、
 * 渠道是 hidden 时作品对外不存在。这些判定只在服务端有一份（`GetDemoForDownload`），
 * 前端再推一遍就是第二份事实源，且推错的后果是「按钮点了 404」。
 *
 * # 为什么是 <a download> 而不是 fetch + Blob
 *
 * 服务端已经给了 `Content-Disposition: attachment`，浏览器会直接落盘。
 * 走 fetch 再 `URL.createObjectURL` 要多占一份内存（作品包可能有几十 MB），
 * 而且丢掉「下载进度」这个浏览器原生能力。
 */
export function DemoDownload({ slug }: { slug: string }) {
  return (
    <p className="mt-4">
      <a
        href={`/download/${encodeURIComponent(slug)}`}
        download
        className="inline-flex items-center rounded-lg border border-gray-300 px-3 py-1.5 text-sm text-gray-700 hover:bg-gray-50 dark:border-gray-700 dark:text-gray-200 dark:hover:bg-gray-800"
      >
        下载作品
      </a>
      <span className="ml-2 text-xs text-gray-500 dark:text-gray-400">
        原始上传包（单文件作品为原文件）
      </span>
    </p>
  );
}

/**
 * 作者的生命周期操作区：下架 / 重新上架 / 删除 / 恢复。
 *
 * # 为什么按钮由服务端给
 *
 * 「此刻能做什么」取决于三个变量的组合：是不是作者、有没有被删、是不是被平台移除。
 * 前端凑不齐这三件事（响应里根本没有 `deletedAt`），自己推就是第二份事实源，
 * 推错的表现是「按钮点了 409」。所以服务端直接给四个布尔（`lifecycleActions`），
 * 这里只负责把它们变成按钮。
 *
 * # 删除为什么要两步
 *
 * 删除对外就是不存在（访客立刻 404），恢复只在窗口内有效。这一步用浏览器原生
 * `confirm` 也行，但那会把「30 天内可恢复」这层信息压成一个干巴巴的问句；
 * 就地展开一行确认，能顺带把这句话说清楚。
 */
export function LifecycleBar({ slug, demo }: { slug: string; demo: DemoDetail }) {
  const queryClient = useQueryClient();
  const [confirming, setConfirming] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const actions = demo.lifecycleActions;

  // 四个动作共用一个 mutation：它们的后续处理完全一样——
  // 刷新详情（按钮位会变）、刷新列表（可见性会变）、清掉错误。
  const run = useMutation({
    mutationFn: async (action: 'archive' | 'unarchive' | 'delete' | 'restore') => {
      const response =
        action === 'archive'
          ? await archiveDemo(slug)
          : action === 'unarchive'
            ? await unarchiveDemo(slug)
            : action === 'delete'
              ? await deleteDemo(slug)
              : await restoreDemo(slug);
      // orval 对非 2xx 也 resolve，必须自己看状态码，否则失败会被当成成功。
      if (response.status !== 200) {
        const message = (response.data as { message?: string } | undefined)?.message;
        throw new Error(message ?? `操作失败（HTTP ${response.status}）`);
      }
      return response;
    },
    onSuccess: async () => {
      setConfirming(false);
      setError(null);
      await queryClient.invalidateQueries({ queryKey: ['demo', slug] });
      await queryClient.invalidateQueries({ queryKey: ['demos'] });
    },
    onError: (err: unknown) => {
      setConfirming(false);
      setError(err instanceof Error ? err.message : '操作失败');
    },
  });

  const buttonClass =
    'inline-flex items-center rounded-lg border border-gray-300 px-3 py-1.5 text-sm text-gray-700 hover:bg-gray-50 disabled:opacity-50 dark:border-gray-700 dark:text-gray-200 dark:hover:bg-gray-800';
  const dangerClass =
    'inline-flex items-center rounded-lg border border-red-300 px-3 py-1.5 text-sm text-red-700 hover:bg-red-50 disabled:opacity-50 dark:border-red-800 dark:text-red-300 dark:hover:bg-red-950';

  const nothingToDo = !actions.archive && !actions.unarchive && !actions.delete && !actions.restore;
  if (nothingToDo && !error) return null;

  return (
    <section className="mt-4" aria-label="作品管理">
      <div className="flex flex-wrap items-center gap-2">
        {actions.archive ? (
          <button
            type="button"
            className={buttonClass}
            disabled={run.isPending}
            onClick={() => run.mutate('archive')}
          >
            下架作品
          </button>
        ) : null}
        {actions.unarchive ? (
          <button
            type="button"
            className={buttonClass}
            disabled={run.isPending}
            onClick={() => run.mutate('unarchive')}
          >
            重新上架
          </button>
        ) : null}
        {actions.restore ? (
          <button
            type="button"
            className={buttonClass}
            disabled={run.isPending}
            onClick={() => run.mutate('restore')}
          >
            恢复作品
          </button>
        ) : null}
        {actions.delete && !confirming ? (
          <button
            type="button"
            className={dangerClass}
            disabled={run.isPending}
            onClick={() => setConfirming(true)}
          >
            删除作品
          </button>
        ) : null}
      </div>

      {confirming ? (
        <div className="mt-3 rounded-lg border border-red-200 bg-red-50 p-3 text-sm dark:border-red-900 dark:bg-red-950">
          <p className="text-red-800 dark:text-red-200">
            删除后这件作品对外立刻不可见。删除前设定的恢复期内可以恢复，过期就再也找不回来了。
          </p>
          <div className="mt-2 flex gap-2">
            <button
              type="button"
              className={dangerClass}
              disabled={run.isPending}
              onClick={() => run.mutate('delete')}
            >
              {run.isPending ? '删除中…' : '确认删除'}
            </button>
            <button
              type="button"
              className={buttonClass}
              disabled={run.isPending}
              onClick={() => setConfirming(false)}
            >
              取消
            </button>
          </div>
        </div>
      ) : null}

      {error ? (
        <p role="alert" className="mt-2 text-sm text-red-700 dark:text-red-300">
          {error}
        </p>
      ) : null}
    </section>
  );
}
