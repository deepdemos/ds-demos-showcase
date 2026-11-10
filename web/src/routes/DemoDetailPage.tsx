import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useEffect, useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router';
import { Button, EmptyState, ErrorState, Rating, Skeleton } from '@/components/ui';
import {
  archiveDemo,
  deleteDemo,
  deleteDemoRating,
  getDemo,
  postDemoView,
  putDemoRating,
  restoreDemo,
  unarchiveDemo,
  type DemoDetail,
} from '@/api';
import { DemoTimeline } from '@/features/demo/timeline-section';
import { RelatedSection } from '@/features/demo/related-section';

type Loaded = { kind: 'ok'; demo: DemoDetail } | { kind: 'missing' };

/**
 * 软删之后服务端不再返回标题（`title` 是空串），页面上就只剩一段说明，
 * 作者只能凭 URL 认人。给一个占位，恢复后标题自然回来（issue #372）。
 */
function displayTitle(demo: Pick<DemoDetail, 'title' | 'lifecycleActions'>): string {
  if (demo.title?.trim()) return demo.title;
  return demo.lifecycleActions.restore ? '已删除的作品' : '未命名作品';
}

function formatDay(iso: string): string {
  return new Date(iso).toLocaleDateString('zh-CN');
}

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

/**
 * 站内还有上一层可以退吗。
 *
 * React Router 把「这是本次会话第几步」记在 history.state.idx 里：从别处点进来
 * 是 1、2、3…，直接在地址栏打开或从外链进来是 0。用它判断，比查 history.length
 * 可靠——后者把站外的历史也算进去了，退一次可能把人送出站。
 */
export function hasInAppHistory(state: unknown): boolean {
  const idx = (state as { idx?: number } | null)?.idx ?? 0;
  return idx > 0;
}

export function DemoDetailPage() {
  const { slug = '' } = useParams();
  const navigate = useNavigate();

  // 「返回」退的是来路，不是写死的作品库。
  //
  // 从排行榜 / 模型页 / 搜索页点进来的读者，本来想回的是那一页；把所有人送回
  // 作品库，等于每点一次详情就丢一次上下文。没有站内上一层时兜底回作品库——
  // 那种情况下 navigate(-1) 会把人送出站。
  const goBack = () => {
    if (hasInAppHistory(window.history.state)) {
      navigate(-1);
      return;
    }
    navigate('/demos');
  };

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
        <Button type="button" inline onClick={goBack}>
          ← 返回上一页
        </Button>
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
            {displayTitle(data.demo)}
          </h1>
          <p className="mt-1 text-xs text-gray-500 dark:text-gray-400">
            作者 {data.demo.author.displayName || data.demo.author.username}
          </p>
          {/*
            发布时间取渠道最早的那一次（publishedAt），不是 demos.created_at：
            「什么时候投的稿」和「什么时候对外能看」是两件事，读者关心后者。
            所以 publishedAt 为空时不能把 created_at 写成「发布」——那是创建时间，
            草稿页上会变成「未发布却写着发布日」（issue #375）。
          */}
          <p className="mt-1 text-xs text-gray-500 dark:text-gray-400">
            {data.demo.publishedAt
              ? `发布 ${formatDay(data.demo.publishedAt)}`
              : `创建于 ${formatDay(data.demo.createdAt)}`}
            {data.demo.updatedAt !== data.demo.createdAt &&
              ` · 更新 ${formatDay(data.demo.updatedAt)}`}
          </p>

          {/* 业务统计：服务端只在「已发布且有对外版本」时给这两个数，
              墓碑与未发布一律为 0，所以这里不用再判状态。 */}
          {data.demo.viewCount > 0 || data.demo.downloadCount > 0 ? (
            <p className="mt-1 text-xs text-gray-500 dark:text-gray-400">
              浏览 {data.demo.viewCount.toLocaleString('zh-CN')} 次 · 下载{' '}
              {data.demo.downloadCount.toLocaleString('zh-CN')} 次
            </p>
          ) : null}

          <RatingBlock slug={slug} demo={data.demo} />

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

          {data.demo.classifications.length > 0 ? (
            <p className="mt-4 flex flex-wrap items-center gap-2 text-sm text-gray-600 dark:text-gray-400">
              分类
              {data.demo.classifications.map((c) => (
                <Link
                  key={`${c.dimension}:${c.slug}`}
                  to={`/demos?${c.dimension}=${encodeURIComponent(c.slug)}`}
                  className="rounded-full border border-gray-200 px-2 py-0.5 text-xs text-gray-700 no-underline hover:border-gray-300 dark:border-gray-800 dark:text-gray-300 dark:hover:border-gray-700"
                >
                  {c.label}
                </Link>
              ))}
            </p>
          ) : null}

          {/* 模型声明是作者自述，不是平台核验过的结论——文案里说清楚，
              否则读者会把它当成「这个作品确实跑在这些模型上」的保证。 */}
          {data.demo.models.length > 0 ? (
            <p className="mt-4 text-sm text-gray-600 dark:text-gray-400">
              模型声明：
              {data.demo.models.map((m, i) => (
                <span key={m.slug}>
                  {i > 0 ? '、' : ''}
                  <Link className="underline" to={`/models/${m.slug}`}>
                    {m.name}
                  </Link>
                </span>
              ))}
              <span className="ml-2 text-xs text-gray-500 dark:text-gray-500">
                （作者自述，未经核验）
              </span>
            </p>
          ) : null}

          {/* 审核态只对作者有意义：访客看到的详情一定来自已通过的那一版，
              服务端对他们恒回 approved（见 handlers.go 的 detailOf）。 */}
          {data.demo.canEdit && data.demo.moderationState !== 'approved' ? (
            <p className="mt-4 rounded-lg border border-sky-300 bg-sky-50 px-3 py-2 text-sm text-sky-900 dark:border-sky-800 dark:bg-sky-950 dark:text-sky-200">
              {moderationBanner(data.demo.moderationState, data.demo.lifecycleState)}
            </p>
          ) : null}

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

          {/* 作品动态只给作者：服务端对非作者一律 404，所以这里也只在作者视图挂载，
              否则每个访客都会多一个必然 404 的请求。 */}
          {data.demo.isAuthor ? <DemoTimeline slug={slug} /> : null}

          {data.demo.downloadable ? <DemoDownload slug={slug} /> : null}

          {/* 外链作品没有本站内容：预览会 302 到作者给的地址，塞进 iframe 大概率被
              对方的 X-Frame-Options 拦成白屏。这里给一个明确的出口，而不是嵌。 */}
          {data.demo.lifecycleState === 'published' ? (
            data.demo.demoType === 'link' ? (
              <section className="mt-6 space-y-2">
                <h2 className="text-base font-semibold">作品在外部站点</h2>
                <p className="m-0 text-sm text-gray-600 dark:text-gray-300">
                  这类作品的内容不在本站，点下面的按钮会跳转到作者提供的地址。
                </p>
                <Link
                  to={`/preview/${encodeURIComponent(slug)}/`}
                  target="_blank"
                  rel="noreferrer"
                  className="inline-flex rounded-lg border border-gray-300 px-4 py-2 text-sm font-medium hover:bg-gray-50 dark:border-gray-700 dark:hover:bg-gray-800"
                >
                  打开作品
                </Link>
              </section>
            ) : (
              <DemoPreview slug={slug} />
            )
          ) : null}

          {/* 相关作品在读不到或为空时自己整块不渲染，所以这里不需要再判一次。 */}
          <RelatedSection slug={slug} />
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

// 审核态提示文案。**必须同时看 lifecycleState**：对外内容由 published_revision_id
// 决定、审核态只描述当前版本（迁移 0014 的版本分离），所以「已发布 + 待审」是
// 作者改稿的正常状态——那时作品还在库里、也还发布着，说「不会出现在作品库里、
// 也不能发布」两句都是假的，而作者的下一步很可能就是再去点一次发布。
function moderationBanner(state: string, lifecycle: string): string {
  if (state === 'pending') {
    return lifecycle === 'published'
      ? '新版本审核中：线上仍是上一版，通过之前这次的改动不会对外生效。'
      : '审核中：通过之前，这条不会出现在作品库里，也不能发布。';
  }
  return lifecycle === 'published'
    ? '新版本未通过审核：线上仍是上一版，改好后重新提交会再次进入待审队列。'
    : '审核未通过：改好之后重新提交，会再次进入待审队列。';
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
/**
 * 评分区：平均分 + 票数 + 五颗星。
 *
 * # 为什么用 PUT 语义
 *
 * 「我对这件作品的分数是 4」是一个状态，不是一条流水。`PUT` 重发多少次结果一样，
 * 所以连点也不会刷出多票（服务端还有 `rater_key` 唯一约束兜底）。
 *
 * # 为什么失败要显示出来
 *
 * 评分是用户主动做的动作。静默失败会让他以为自己打了 5 分而实际没打上——
 * 这与浏览计数相反（那个是页面自己顺带记的，失败不该打扰人）。
 *
 * # 为什么点同一颗星是撤销
 *
 * 改主意的唯一入口。没有它，用户想取消评分就得去别处找按钮，而页面没有那个按钮。
 */
function RatingBlock({ slug, demo }: { slug: string; demo: DemoDetail }) {
  const queryClient = useQueryClient();
  const [error, setError] = useState<string | undefined>();

  function refresh() {
    // 只重取详情：均分与「我打了几分」都在里面。直接改缓存会让两处口径
    // （详情 vs 服务端重算）漂开，而这个页面本来就依赖服务端算好的数字。
    void queryClient.invalidateQueries({ queryKey: ['demo', slug] });
  }

  const rate = useMutation({
    mutationFn: async (score: number) => {
      const response =
        score > 0 ? await putDemoRating(slug, { score }) : await deleteDemoRating(slug);
      if (response.status !== 200) {
        throw new Error(
          score > 0 ? `打分失败（HTTP ${response.status}）` : `撤销失败（HTTP ${response.status}）`,
        );
      }
      return response.data;
    },
    onSuccess: () => {
      setError(undefined);
      refresh();
    },
    onError: (err: Error) => setError(err.message),
  });

  const { average, count, mine } = demo.rating;

  return (
    <div className="mt-4 flex flex-wrap items-center gap-x-4 gap-y-2">
      <Rating
        label="给这个作品打分"
        value={mine ?? 0}
        onChange={(score) => rate.mutate(score)}
        disabled={rate.isPending}
        showValueText
      />
      <span className="text-xs text-gray-500 dark:text-gray-400">
        {count > 0 ? `平均 ${average.toFixed(2)} 分，${count} 人评过` : '还没有人评分'}
      </span>
      {error ? (
        <span role="alert" className="text-xs text-red-600 dark:text-red-400">
          {error}
        </span>
      ) : null}
    </div>
  );
}

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

  const nothingToDo = !actions.archive && !actions.unarchive && !actions.delete && !actions.restore;
  if (nothingToDo && !error) return null;

  return (
    <section className="mt-4" aria-label="作品管理">
      <div className="flex flex-wrap items-center gap-2">
        {actions.archive ? (
          <Button type="button" disabled={run.isPending} onClick={() => run.mutate('archive')}>
            下架作品
          </Button>
        ) : null}
        {actions.unarchive ? (
          <Button type="button" disabled={run.isPending} onClick={() => run.mutate('unarchive')}>
            重新上架
          </Button>
        ) : null}
        {actions.restore ? (
          <Button type="button" disabled={run.isPending} onClick={() => run.mutate('restore')}>
            恢复作品
          </Button>
        ) : null}
        {actions.delete && !confirming ? (
          <Button type="button" danger disabled={run.isPending} onClick={() => setConfirming(true)}>
            删除作品
          </Button>
        ) : null}
      </div>

      {confirming ? (
        <div className="mt-3 rounded-lg border border-red-200 bg-red-50 p-3 text-sm dark:border-red-900 dark:bg-red-950">
          <p className="text-red-800 dark:text-red-200">
            删除后这件作品对外立刻不可见。删除前设定的恢复期内可以恢复，过期就再也找不回来了。
          </p>
          <div className="mt-2 flex gap-2">
            <Button
              type="button"
              danger
              disabled={run.isPending}
              onClick={() => run.mutate('delete')}
            >
              {run.isPending ? '删除中…' : '确认删除'}
            </Button>
            <Button type="button" disabled={run.isPending} onClick={() => setConfirming(false)}>
              取消
            </Button>
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
