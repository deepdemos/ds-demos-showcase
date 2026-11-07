import { useQuery } from '@tanstack/react-query';
import { Link, useParams } from 'react-router';

import { getModel, type ModelProfile, type ModelSpecs } from '@/api';
import { EmptyState, ErrorState, Skeleton } from '@/components/ui';

/**
 * 模型页（模型域 §5.1）。
 *
 * # 三块分开写，不合并成一个「总分」
 *
 * 档案、站内表现、第三方基准的置信度差得远：档案是上游给的客观值，站内表现是
 * 我们自己的样本（还可能是 0 票），基准是别人榜单的原始分。把三者压成一个数字，
 * 读者就无法判断「这个 82 分」到底是模型强，还是仅仅投的人多。
 *
 * # 「还没有人评分」不是「0 分」
 *
 * 服务端用 `site.scored` 区分这两件事。0 票时显示「还没有人评分」而不是星星全灭——
 * 前者是事实，后者是评价。
 *
 * # 缺值显示「—」，不显示 0
 *
 * 档案里没给的字段一律 `—`。显示 0 会被当成真数据（「上下文 0」读起来像限制为 0）。
 */
export function ModelPage() {
  const { slug = '' } = useParams();

  const query = useQuery({
    queryKey: ['model', slug],
    queryFn: async () => {
      const response = await getModel(slug);
      if (response.status === 404) throw new NotFoundError();
      if (response.status !== 200) throw new Error(`加载失败（HTTP ${response.status}）`);
      return response.data;
    },
    retry: false,
  });

  if (query.isPending) {
    return (
      <div className="mx-auto w-full max-w-3xl px-4 py-10">
        <Skeleton lines={8} />
      </div>
    );
  }

  if (query.isError) {
    if (query.error instanceof NotFoundError) {
      return (
        <div className="mx-auto w-full max-w-3xl px-4 py-10">
          <EmptyState
            title="没有这个模型"
            description="链接可能过期了，或者这个模型还没进目录。"
            action={
              <Link className="text-sm underline" to="/models">
                看模型目录
              </Link>
            }
          />
        </div>
      );
    }
    return (
      <div className="mx-auto w-full max-w-3xl px-4 py-10">
        <ErrorState
          title="模型页加载失败"
          description={query.error instanceof Error ? query.error.message : undefined}
          onRetry={() => void query.refetch()}
          retrying={query.isFetching}
        />
      </div>
    );
  }

  const profile = query.data;
  const { model, specs, site } = profile;

  return (
    <div className="mx-auto w-full max-w-3xl px-4 py-10">
      <header className="flex flex-col gap-1">
        <h1 className="text-2xl font-semibold text-gray-900 dark:text-gray-100">{model.name}</h1>
        <p className="text-sm text-gray-500 dark:text-gray-400">
          {model.vendor || '厂商未知'} · <code className="text-xs">{model.slug}</code>
          {' · '}
          {statusLabel(model.status)}
        </p>
        {model.description && (
          <p className="mt-2 text-sm text-gray-700 dark:text-gray-300">{model.description}</p>
        )}
        {model.mergedIntoSlug && (
          <p className="mt-1 text-sm text-amber-700 dark:text-amber-400">
            这个模型已合并到{' '}
            <Link className="underline" to={`/models/${model.mergedIntoSlug}`}>
              {model.mergedIntoSlug}
            </Link>
            ，本页只保留历史。
          </p>
        )}
      </header>

      <Section title="档案">
        {specs === null ? (
          <p className="text-sm text-gray-500 dark:text-gray-400">
            还没有这个模型的档案。上游目录里可能有，管理员可以从导入页补录。
          </p>
        ) : (
          <SpecTable specs={specs} />
        )}
      </Section>

      <Section title="站内表现">
        <dl className="grid grid-cols-2 gap-3 text-sm sm:grid-cols-3">
          <Stat label="公开作品" value={`${site.demoCount} 件`} />
          <Stat label="评分票数" value={`${site.votes} 票`} />
          <Stat
            label="社区分"
            value={site.scored ? site.score.toFixed(2) : '还没有人评分'}
            hint={site.scored ? `收缩权重 ${site.priorWeight.toFixed(1)}` : undefined}
          />
        </dl>

        {profile.providers.length > 0 && (
          <div className="mt-4">
            <h3 className="text-sm font-medium text-gray-800 dark:text-gray-200">来源分布</h3>
            <p className="mt-1 text-xs text-gray-500 dark:text-gray-400">
              只做展示，不改变上面的社区分。
            </p>
            <table className="mt-2 w-full text-left text-sm">
              <thead className="text-xs text-gray-500 dark:text-gray-400">
                <tr>
                  <th className="py-1 font-normal">服务方</th>
                  <th className="py-1 font-normal">作品</th>
                  <th className="py-1 font-normal">票数</th>
                </tr>
              </thead>
              <tbody>
                {profile.providers.map((slice) => (
                  <tr
                    key={slice.providerSlug || 'unspecified'}
                    className="border-t border-gray-100 dark:border-gray-800"
                  >
                    <td className="py-1">
                      {slice.providerSlug ? slice.providerName : '来源未标注'}
                    </td>
                    <td className="py-1">{slice.demoCount}</td>
                    <td className="py-1">{slice.votes}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}

        <div className="mt-5">
          <h3 className="text-sm font-medium text-gray-800 dark:text-gray-200">
            用了这个模型的作品
          </h3>
          {profile.demos.length === 0 ? (
            <p className="mt-1 text-sm text-gray-500 dark:text-gray-400">
              还没有公开作品声明使用这个模型。
            </p>
          ) : (
            <ul className="mt-2 flex flex-col gap-1">
              {profile.demos.map((demo) => (
                <li key={demo.slug} className="text-sm">
                  <Link className="underline" to={`/demos/${demo.slug}`}>
                    {demo.title}
                  </Link>
                  <span className="ml-2 text-xs text-gray-500 dark:text-gray-400">
                    {demo.ratingCount > 0
                      ? `${demo.ratingAverage.toFixed(1)} 分 · ${demo.ratingCount} 票`
                      : '还没有人评分'}
                  </span>
                </li>
              ))}
            </ul>
          )}
        </div>
      </Section>

      <Section title="第三方基准">
        {profile.benchmarks.length === 0 ? (
          <p className="text-sm text-gray-500 dark:text-gray-400">
            还没有接入任何基准榜。我们不自己造分数——榜单一律等归一化方案定下来再算。
          </p>
        ) : (
          <table className="w-full text-left text-sm">
            <thead className="text-xs text-gray-500 dark:text-gray-400">
              <tr>
                <th className="py-1 font-normal">榜单</th>
                <th className="py-1 font-normal">指标</th>
                <th className="py-1 font-normal">原始分</th>
                <th className="py-1 font-normal">名次</th>
                <th className="py-1 font-normal">参赛</th>
              </tr>
            </thead>
            <tbody>
              {profile.benchmarks.map((b) => (
                <tr
                  key={`${b.benchmarkSlug}-${b.version}-${b.metricKey}`}
                  className="border-t border-gray-100 dark:border-gray-800"
                >
                  <td className="py-1">{b.benchmarkName}</td>
                  <td className="py-1">{b.metricLabel}</td>
                  <td className="py-1">
                    {b.rawScore}
                    {b.unit ? ` ${b.unit}` : ''}
                  </td>
                  <td className="py-1">{b.rank ?? '—'}</td>
                  <td className="py-1">{b.participantCount ?? '—'}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Section>
    </div>
  );
}

class NotFoundError extends Error {}

function statusLabel(status: ModelProfile['model']['status']): string {
  switch (status) {
    case 'active':
      return '可用';
    case 'candidate':
      return '候选（等人确认）';
    case 'unverified':
      return '未核验';
    case 'deprecated':
      return '已弃用';
    default:
      return status;
  }
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="mt-8">
      <h2 className="text-lg font-semibold text-gray-900 dark:text-gray-100">{title}</h2>
      <div className="mt-3">{children}</div>
    </section>
  );
}

function Stat({ label, value, hint }: { label: string; value: string; hint?: string }) {
  return (
    <div>
      <dt className="text-xs text-gray-500 dark:text-gray-400">{label}</dt>
      <dd className="text-sm text-gray-900 dark:text-gray-100">{value}</dd>
      {hint && <dd className="text-xs text-gray-400 dark:text-gray-500">{hint}</dd>}
    </div>
  );
}

function SpecTable({ specs }: { specs: ModelSpecs }) {
  const rows: Array<[string, string]> = [
    [
      '上下文窗口',
      specs.contextWindow == null ? '—' : `${specs.contextWindow.toLocaleString()} tokens`,
    ],
    ['最大输出', specs.maxOutput == null ? '—' : `${specs.maxOutput.toLocaleString()} tokens`],
    ['参数量', paramText(specs.paramTotalB, specs.paramActiveB, specs.paramKind)],
    ['发布日期', specs.releaseDate ?? '—'],
    ['知识截止', specs.knowledgeCutoff ?? '—'],
    ['开放权重', specs.openWeights == null ? '—' : specs.openWeights === 1 ? '是' : '否'],
    ['价格（输入 / 输出）', priceText(specs.costIn, specs.costOut)],
    ['许可', specs.license ?? '—'],
    ['输入模态', specs.modalitiesIn || '—'],
    ['输出模态', specs.modalitiesOut || '—'],
    ['来源', specs.source || '—'],
  ];
  return (
    <table className="w-full text-left text-sm">
      <tbody>
        {rows.map(([label, value]) => (
          <tr key={label} className="border-t border-gray-100 dark:border-gray-800">
            <th className="w-40 py-1 font-normal text-gray-500 dark:text-gray-400">{label}</th>
            <td className="py-1 text-gray-900 dark:text-gray-100">{value}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

function paramText(
  total: number | null | undefined,
  active: number | null | undefined,
  kind: string | null | undefined,
): string {
  if (total == null && active == null) return '—';
  const parts: string[] = [];
  if (total != null) parts.push(`总 ${total}B`);
  if (active != null) parts.push(`激活 ${active}B`);
  const text = parts.join(' / ');
  return kind ? `${text}（${kind}）` : text;
}

function priceText(costIn: number | null | undefined, costOut: number | null | undefined): string {
  if (costIn == null && costOut == null) return '—';
  const fmt = (v: number | null | undefined) => (v == null ? '—' : `${v}`);
  return `${fmt(costIn)} / ${fmt(costOut)}（每百万 token，上游口径）`;
}
