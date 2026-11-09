import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { addDemoChannel, listDemoChannels, removeDemoChannel } from '@/api';
import type { DemoChannel } from '@/api';
import { Button } from '@/components/ui/button';
import { EmptyState } from '@/components/ui/empty-state';
import { ErrorState } from '@/components/ui/error-state';
import { Skeleton } from '@/components/ui/skeleton';

/**
 * 渠道面板：这件作品现在挂在哪些渠道上，还能挂到哪、从哪撤下来。
 *
 * # 为什么要有它
 *
 * 渠道端点（`POST/DELETE /admin/demos/{slug}/channels`）做完只有 curl 能调。
 * 「作品发过一次就再也发不到第二个渠道」这个限制（松点 26）在接口层已经解掉，
 * 但界面上没有入口，等于只解了一半：真正的管理员不会开终端。
 *
 * # 为什么读列表要另开一个端点
 *
 * 详情是公开读的出口，字段面讲的是「读者看到的这一件作品」；渠道是运营视角。
 * 塞进详情要么让公开字段变胖，要么要按角色裁剪同一份 schema。
 *
 * # 为什么撤下不做二次确认
 *
 * 撤下只删一条渠道行，作品还在、内容还在，再点一次「上架」就回来了——
 * 这不是删除，给它加确认框只会训练人无脑点「确定」。真正的破坏性动作（删作品）
 * 才需要确认，那里已经有「要点两次」的交互。
 */

const CHANNEL_LABELS: Record<string, string> = {
  deep: '主站',
  astra: 'Astra 橱窗',
};

/** 可追加的渠道（`channels` 表里 active 的那些；这里只做展示名映射）。 */
const ADDABLE = [
  { value: 'deep', label: '主站' },
  { value: 'astra', label: 'Astra 橱窗' },
];

const VISIBILITY_LABELS: Record<string, string> = {
  listed: '公开列出',
  unlisted: '有链接可见',
  hidden: '隐藏',
};

/** 与发布卡片同一张表：优先用后端的中文 message，没有才落到这里。 */
const CHANNEL_ERROR: Record<number, string> = {
  400: '渠道或可见性不合法',
  401: '登录已过期，请重新登录',
  403: '没有发布权限',
  404: '作品不存在',
  409: '这件作品还没有对外版本，或这是最后一个渠道',
  503: '服务暂时不可用，请稍后重试',
};

function errorText(err: unknown): string {
  if (err && typeof err === 'object' && 'status' in err) {
    const status = (err as { status: number }).status;
    return CHANNEL_ERROR[status] ?? `渠道操作失败（HTTP ${status}）`;
  }
  return '渠道操作失败，请重试';
}

/** orval 生成的函数对非 2xx 也 resolve，不自己翻成异常的话所有失败都静默。 */
async function unwrap<T extends { status: number }>(promise: Promise<T>): Promise<T> {
  const res = await promise;
  if (res.status >= 400) {
    throw Object.assign(new Error(`HTTP ${res.status}`), { status: res.status });
  }
  return res;
}

export function ChannelPanel({ slug }: { slug: string }) {
  const queryClient = useQueryClient();

  const query = useQuery({
    queryKey: ['channels', slug],
    queryFn: async () => {
      const res = await unwrap(listDemoChannels(slug));
      // 生成物把成功体与错误体并成一个联合类型；status 已经保证了是 2xx，
      // 这里再做一次形状判定，免得把 Error 当成列表读。
      if (!('items' in res.data)) {
        throw new Error('渠道列表的形状不对');
      }
      return res.data.items;
    },
    staleTime: 30 * 1000,
  });

  const invalidate = () => queryClient.invalidateQueries({ queryKey: ['channels', slug] });

  const add = useMutation({
    mutationFn: async (channel: string) => {
      // astra 是英文橱窗，渠道规则要求 lang='en'——这是渠道属性，不是用户的选择。
      const lang = channel === 'astra' ? 'en' : 'zh';
      await unwrap(addDemoChannel(slug, { channel, visibility: 'unlisted', lang }));
    },
    onSuccess: invalidate,
  });

  const remove = useMutation({
    mutationFn: async (channel: string) => {
      await unwrap(removeDemoChannel(slug, channel));
    },
    onSuccess: invalidate,
  });

  if (query.isPending) {
    return <Skeleton lines={2} />;
  }
  if (query.isError) {
    return (
      <ErrorState
        title="渠道读不到"
        description="这件作品的渠道列表没有加载成功。"
        onRetry={() => void query.refetch()}
        retrying={query.isFetching}
      />
    );
  }

  const channels: DemoChannel[] = query.data;
  const existing = new Set(channels.map((item) => item.channel));
  const addable = ADDABLE.filter((item) => !existing.has(item.value));
  const busy = add.isPending || remove.isPending;

  return (
    <section className="space-y-3" aria-labelledby={`channels-heading-${slug}`}>
      <h3 id={`channels-heading-${slug}`} className="text-sm font-medium">
        渠道
      </h3>

      {channels.length === 0 ? (
        <EmptyState title="还没有渠道" description="发布过一次之后才会有渠道行。" />
      ) : (
        <ul className="list-none space-y-2 p-0">
          {channels.map((item) => (
            <li key={item.channel} className="flex items-center gap-3 text-sm">
              <span className="font-medium">{CHANNEL_LABELS[item.channel] ?? item.channel}</span>
              <span className="text-neutral-500 dark:text-neutral-400">
                {VISIBILITY_LABELS[item.visibility] ?? item.visibility} · {item.lang}
              </span>
              <Button
                type="button"
                danger
                disabled={busy || channels.length === 1}
                onClick={() => remove.mutate(item.channel)}
              >
                撤下
              </Button>
              {channels.length === 1 ? (
                <span className="text-xs text-neutral-500 dark:text-neutral-400">
                  最后一个渠道不能撤
                </span>
              ) : null}
            </li>
          ))}
        </ul>
      )}

      {addable.length > 0 ? (
        <div className="flex flex-wrap gap-2">
          {addable.map((item) => (
            <Button
              key={item.value}
              type="button"
              disabled={busy}
              onClick={() => add.mutate(item.value)}
            >
              上架到 {item.label}
            </Button>
          ))}
        </div>
      ) : null}

      {add.isError || remove.isError ? (
        <p role="alert" className="text-sm text-red-600 dark:text-red-400">
          {errorText(add.isError ? add.error : remove.error)}
        </p>
      ) : null}
    </section>
  );
}
