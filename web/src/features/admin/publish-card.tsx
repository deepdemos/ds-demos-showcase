import { useMutation } from '@tanstack/react-query';
import { useState } from 'react';
import { Link } from 'react-router';
import { publishDemo } from '@/api';
import { ChannelPanel } from '@/features/admin/channel-panel';
import type { PublishRequestLang, PublishRequestVisibility } from '@/api';
import { Button } from '@/components/ui/button';
import { Select } from '@/components/ui/select';
import type { SelectOption } from '@/components/ui/select';

/**
 * 发布失败时的兜底文案。优先用后端返回的 message（中文、带具体原因），
 * 只有响应体没有 message 时才落到这张表——与审核页的 DECIDE_ERROR 同口径。
 */
const PUBLISH_ERROR: Record<number, string> = {
  400: '请求参数不合法（渠道 / 可见性 / 语言）',
  401: '登录已过期，请重新登录',
  403: '没有发布权限',
  404: '作品不存在',
  409: '这条作品现在不在可发布的状态，刷新后再看一遍',
  503: '服务暂时不可用，请稍后重试',
};

/** 渠道主数据来自 `channels` 表（迁移 0010），这里只做展示名映射。 */
const CHANNELS = [
  { value: 'deep', label: '主站' },
  { value: 'astra', label: 'Astra 橱窗' },
];

const VISIBILITIES = [
  { value: 'listed', label: '公开列出（会出现在作品库）' },
  { value: 'unlisted', label: '有链接可见（不上列表）' },
  { value: 'hidden', label: '隐藏' },
] as const satisfies readonly SelectOption[];

const LANGS = [
  { value: 'zh', label: '中文' },
  { value: 'en', label: '英文' },
] as const satisfies readonly SelectOption[];

const VISIBILITY_VALUES = VISIBILITIES.map((item) => item.value);
const LANG_VALUES = LANGS.map((item) => item.value);

/**
 * `Select` 的 onChange 只保证给回一个 string（控件库不该认识业务枚举），所以这里
 * 把它收窄回契约枚举再落进 state。用 `some` 写类型谓词而不是 `as`：值和枚举对不上
 * 时**什么都不做**，而不是把一个假值塞进请求体，让后端去报 400。
 */
function isOneOf<T extends string>(pool: readonly T[], value: string): value is T {
  return pool.some((item) => item === value);
}

/**
 * 审核通过之后的发布卡片：选渠道 / 可见性 / 语言，调 `POST /admin/demos/{slug}/publish`。
 *
 * # 为什么这一段必须存在
 *
 * 「审核通过 ≠ 已发布」是 Demo 域 §6.1 的规则，两个动作是两个端点（`demo.review` /
 * `demo.publish`）。审核队列只列待审作品，一通过就从队列里消失；如果没有这块卡片，
 * 管理员点完「通过」就再也找不到这条作品，它会永远停在 `draft`——我在做闭环 e2e 时
 * 正是卡在这里发现的。
 *
 * # 为什么 astra 会把语言锁成 en
 *
 * 渠道规则（Demo 域 §7.4）：`astra` 是英文橱窗，要求 `lang='en'`。这是渠道的属性，
 * 不是用户的选择，所以切到 astra 时直接改语言并禁用选择，而不是让用户选完再报错。
 *
 * # 为什么发布后不把卡片移除
 *
 * 发布是这条作品在这个界面上的终点，但「刚才发到哪了」需要看得见——移除后如果
 * 没记下来，管理员会怀疑自己到底有没有点成功。所以留在原地、换成结果态。
 */
export function PublishCard({ slug, title }: { slug: string; title: string }) {
  const [channel, setChannel] = useState('deep');
  const [visibility, setVisibility] = useState<PublishRequestVisibility>('listed');
  const [lang, setLang] = useState<PublishRequestLang>('zh');

  const publish = useMutation({
    // 与审核页同样的理由：orval 生成的函数对非 2xx 也 resolve，不自己翻成异常的话
    // 409 / 403 / 503 全都没有任何提示。
    mutationFn: async () => {
      const res = await publishDemo(slug, { channel, visibility, lang });
      if (res.status !== 200) {
        const message =
          typeof res.data === 'object' && res.data && 'message' in res.data
            ? String((res.data as { message?: unknown }).message)
            : null;
        throw new Error(message || PUBLISH_ERROR[res.status] || `发布失败（${res.status}）`);
      }
      return res;
    },
  });

  const channelLabel = CHANNELS.find((item) => item.value === channel)?.label ?? channel;

  if (publish.isSuccess) {
    return (
      <li className="rounded-lg border border-green-300 bg-green-50/50 p-4 dark:border-green-900 dark:bg-green-950/30">
        <p className="text-sm">
          <span className="font-medium">{title}</span> 已发布到「{channelLabel}」。
        </p>
        <p className="mt-1 text-sm text-neutral-600 dark:text-neutral-300">
          <Link to={`/demos/${slug}`} className="underline">
            查看作品页
          </Link>
        </p>
        {/* 发布成功之后才有渠道行，所以面板放在这里：管理员发完主站就能顺手挂第二个渠道。 */}
        <div className="mt-3 border-t border-green-200 pt-3 dark:border-green-900">
          <ChannelPanel slug={slug} />
        </div>
      </li>
    );
  }

  return (
    <li className="rounded-lg border border-neutral-200 p-4 dark:border-neutral-800">
      <h3 className="truncate font-medium">{title}</h3>

      <div className="mt-3 grid gap-3 sm:grid-cols-3">
        <label className="block text-sm" htmlFor={`publish-channel-${slug}`}>
          <span className="text-neutral-500 dark:text-neutral-400">渠道</span>
          <Select
            id={`publish-channel-${slug}`}
            value={channel}
            options={CHANNELS}
            onChange={(value) => {
              setChannel(value);
              // 渠道规则：astra 是英文橱窗。切过去的同时把语言摆正，
              // 免得用户先看到一个不成立的组合。
              if (value === 'astra') setLang('en');
            }}
            disabled={publish.isPending}
          />
        </label>

        <label className="block text-sm" htmlFor={`publish-visibility-${slug}`}>
          <span className="text-neutral-500 dark:text-neutral-400">可见性</span>
          <Select
            id={`publish-visibility-${slug}`}
            value={visibility}
            options={VISIBILITIES}
            onChange={(value) => {
              if (isOneOf(VISIBILITY_VALUES, value)) setVisibility(value);
            }}
            disabled={publish.isPending}
          />
        </label>

        <label className="block text-sm" htmlFor={`publish-lang-${slug}`}>
          <span className="text-neutral-500 dark:text-neutral-400">语言</span>
          <Select
            id={`publish-lang-${slug}`}
            value={lang}
            options={LANGS}
            onChange={(value) => {
              if (isOneOf(LANG_VALUES, value)) setLang(value);
            }}
            disabled={publish.isPending || channel === 'astra'}
          />
        </label>
      </div>

      {channel === 'astra' ? (
        <p className="mt-2 text-xs text-neutral-500 dark:text-neutral-400">
          Astra 是英文橱窗，渠道规则要求语言为英文，已自动切到 en。
        </p>
      ) : null}

      {publish.isError ? (
        <p role="alert" className="mt-2 text-sm text-red-600 dark:text-red-400">
          {publish.error instanceof Error ? publish.error.message : '发布失败，请重试'}
        </p>
      ) : null}

      <div className="mt-3">
        <Button primary disabled={publish.isPending} onClick={() => publish.mutate()}>
          {publish.isPending ? '发布中…' : '发布'}
        </Button>
      </div>
    </li>
  );
}
