import { Link, useParams } from 'react-router';
import { useQuery } from '@tanstack/react-query';
import { EmptyState, ErrorState, Skeleton } from '@/components/ui';
import { Page } from '@/components/layout/page';
import { getBenchmark, type BenchmarkDetail, type BenchmarkVersionSummary } from '@/api';
import { tierLabel } from '@/content/benchmark-tier';

/**
 * 单张基准的详情（模型域 §5.6）。
 *
 * 页面按**陈列档位**分三种形状，而不是所有档位共用一张空表（#573）：
 * - `table`：概述 / 最新版本 / 指标表 / 版本历史 —— 我们收了分数，正常陈列；
 * - `cite`：概述 / 引用块 —— **不渲染指标表**，因为源方不允许结构化再发布，写「不收录」并给官方外链与署名；
 * - `link`：概述 / 外链块 —— 连版本历史也不给，这张榜我们没有可陈述的口径。
 *
 * 两条口径写在页面上，不写在注释里：
 * - 厂商自报（`self_reported`）单独分组，不进指标表 —— 两种凭据不可比（MODEL-020）。
 * - 区间与样本量缺失就显示 `—`：**不补零、不推断**。
 */

function formatDay(value: string | null | undefined): string {
  if (!value) return '—';
  return new Date(value).toLocaleDateString('zh-CN');
}

function formatNumber(value: number | null | undefined): string {
  if (value === null || value === undefined) return '—';
  return Number.isInteger(value) ? String(value) : String(Math.round(value * 100) / 100);
}

function formatRange(lower: number | null | undefined, upper: number | null | undefined): string {
  if (lower === null || lower === undefined) return '—';
  if (upper === null || upper === undefined) return '—';
  return formatNumber(lower) + ' – ' + formatNumber(upper);
}

function latestThirdParty(
  versions: BenchmarkVersionSummary[],
): BenchmarkVersionSummary | undefined {
  return versions.find((version) => version.sourceType !== 'self_reported');
}

function OverviewSection({ detail }: { detail: BenchmarkDetail }) {
  return (
    <section>
      <h2 className="text-base font-semibold text-gray-900">概述</h2>
      <dl className="mt-3 grid grid-cols-1 gap-2 text-sm sm:grid-cols-2">
        <div>
          <dt className="text-gray-500">发布方</dt>
          <dd className="m-0 text-gray-900">{detail.publisher || '—'}</dd>
        </div>
        <div>
          <dt className="text-gray-500">方向</dt>
          <dd className="m-0 text-gray-900">{detail.category || 'general'}</dd>
        </div>
        <div>
          <dt className="text-gray-500">官网</dt>
          <dd className="m-0">
            {detail.homepageUrl ? (
              <a
                href={detail.homepageUrl}
                rel="noreferrer noopener"
                target="_blank"
                className="text-gray-900 underline-offset-2 hover:underline"
              >
                {detail.homepageUrl}
              </a>
            ) : (
              '—'
            )}
          </dd>
        </div>
        <div>
          <dt className="text-gray-500">陈列档位</dt>
          <dd className="m-0 text-gray-900">{tierLabel[detail.displayTier]}</dd>
        </div>
      </dl>
      {detail.attribution || detail.license ? (
        <p className="mt-3 text-xs text-gray-500">
          {detail.attribution ? '数据来源：' + detail.attribution : ''}
          {detail.attribution && detail.license ? ' · ' : ''}
          {detail.license ? (
            detail.licenseUrl ? (
              <a
                href={detail.licenseUrl}
                rel="noreferrer noopener"
                target="_blank"
                className="underline-offset-2 hover:underline"
              >
                {detail.license}
              </a>
            ) : (
              detail.license
            )
          ) : (
            ''
          )}
        </p>
      ) : null}
    </section>
  );
}

function LatestVersionSection({ current }: { current: BenchmarkVersionSummary | undefined }) {
  return (
    <section>
      <h2 className="text-base font-semibold text-gray-900">最新版本</h2>
      {current ? (
        <p className="mt-2 text-sm text-gray-600">
          {current.version} · {formatDay(current.asOf)} · 参赛{' '}
          {formatNumber(current.participantCount)} ·{' '}
          {current.coverage === 'full' ? '全量覆盖' : '部分覆盖'}
          {current.sourceUrl ? (
            <>
              {' · '}
              <a
                href={current.sourceUrl}
                rel="noreferrer noopener"
                target="_blank"
                className="underline-offset-2 hover:underline"
              >
                原始榜单
              </a>
            </>
          ) : null}
        </p>
      ) : (
        <p className="mt-2 text-sm text-gray-600">还没有第三方版本。</p>
      )}
    </section>
  );
}

function MetricsSection({ metrics }: { metrics: BenchmarkDetail['metrics'] }) {
  return (
    <section>
      <h2 className="text-base font-semibold text-gray-900">指标</h2>
      {metrics.length === 0 ? (
        <p className="mt-2 text-sm text-gray-600">这张榜还没有收录分数。</p>
      ) : (
        metrics.map((metric) => (
          <div key={metric.key} className="mt-4">
            <h3 className="text-sm font-medium text-gray-900">
              {metric.label}
              {metric.isPrimary ? '（主榜）' : ''}
              <span className="ml-2 text-xs text-gray-500">{metric.unit}</span>
            </h3>
            {metric.scores.length === 0 ? (
              <p className="mt-1 text-sm text-gray-600">还没有收录分数。</p>
            ) : (
              <div className="mt-2 overflow-x-auto">
                <table className="w-full min-w-[32rem] border-collapse text-sm">
                  <thead>
                    <tr className="border-b border-gray-200 text-left text-xs text-gray-500">
                      <th scope="col" className="py-2 pr-3 font-medium">
                        模型
                      </th>
                      <th scope="col" className="py-2 pr-3 font-medium">
                        名次
                      </th>
                      <th scope="col" className="py-2 pr-3 font-medium">
                        分数
                      </th>
                      <th scope="col" className="py-2 pr-3 font-medium">
                        区间
                      </th>
                      <th scope="col" className="py-2 font-medium">
                        样本量
                      </th>
                    </tr>
                  </thead>
                  <tbody>
                    {metric.scores.map((score) => (
                      <tr key={score.externalName} className="border-b border-gray-100">
                        <td className="py-2 pr-3 text-gray-900">
                          {score.modelSlug ? (
                            <Link
                              to={'/models/' + score.modelSlug}
                              className="underline-offset-2 hover:underline"
                            >
                              {score.modelName ?? score.externalName}
                            </Link>
                          ) : (
                            // 未解析到站内模型时不编链接：外部名只是上游的写法。
                            score.externalName
                          )}
                        </td>
                        <td className="py-2 pr-3 text-gray-600">{formatNumber(score.rank)}</td>
                        <td className="py-2 pr-3 text-gray-600">{formatNumber(score.rawScore)}</td>
                        <td className="py-2 pr-3 text-gray-600">
                          {formatRange(score.uncertaintyLower, score.uncertaintyUpper)}
                        </td>
                        <td className="py-2 text-gray-600">{formatNumber(score.sampleSize)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </div>
        ))
      )}
    </section>
  );
}

function VersionHistorySection({
  thirdParty,
  selfReported,
}: {
  thirdParty: BenchmarkVersionSummary[];
  selfReported: BenchmarkVersionSummary[];
}) {
  return (
    <section>
      <h2 className="text-base font-semibold text-gray-900">版本历史</h2>
      <p className="mt-1 text-xs text-gray-500">
        第三方榜单与厂商自报分开列：后者只说明厂商怎么说，不进上面的指标表。
      </p>
      <h3 className="mt-3 text-sm font-medium text-gray-900">第三方榜单</h3>
      {thirdParty.length === 0 ? (
        <p className="mt-1 text-sm text-gray-600">还没有第三方版本。</p>
      ) : (
        <ul className="mt-1 list-none p-0 text-sm text-gray-600">
          {thirdParty.map((version) => (
            <li key={version.version} className="border-b border-gray-100 py-2">
              {version.version} · {formatDay(version.asOf)} · 参赛{' '}
              {formatNumber(version.participantCount)} ·{' '}
              {version.coverage === 'full' ? '全量' : '部分'}
            </li>
          ))}
        </ul>
      )}
      <h3 className="mt-4 text-sm font-medium text-gray-900">厂商自报</h3>
      {selfReported.length === 0 ? (
        <p className="mt-1 text-sm text-gray-600">没有厂商自报版本。</p>
      ) : (
        <ul className="mt-1 list-none p-0 text-sm text-gray-600">
          {selfReported.map((version) => (
            <li key={version.version} className="border-b border-gray-100 py-2">
              {version.version} · {formatDay(version.asOf)} · 参赛{' '}
              {formatNumber(version.participantCount)}
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

/**
 * `cite` 档：**不渲染指标表**。读者看到一张空表，会读成「我们的数据缺了」；
 * 实际是源方不允许把分数结构化再发布 —— 这两件事必须说清楚，不能糊过去。
 */
function CiteNotice({ detail }: { detail: BenchmarkDetail }) {
  return (
    <section>
      <h2 className="text-base font-semibold text-gray-900">本站不收录这张榜的分数</h2>
      <p className="mt-2 text-sm text-gray-600">
        这张榜的源方要么不允许把分数结构化再发布，要么各家提交口径不统一。所以站内只做引用，
        不做分数陈列 —— 想比较请到官方榜单去看。
      </p>
      {detail.homepageUrl ? (
        <p className="mt-2 text-sm text-gray-600">
          官方榜单：
          <a
            href={detail.homepageUrl}
            rel="noreferrer noopener"
            target="_blank"
            className="text-gray-900 underline-offset-2 hover:underline"
          >
            {detail.homepageUrl}
          </a>
        </p>
      ) : null}
      {detail.attribution ? (
        <p className="mt-2 text-xs text-gray-500">引用要求署名：{detail.attribution}</p>
      ) : null}
    </section>
  );
}

/** `link` 档：连版本与指标都不给 —— 这张榜我们没有可陈述的口径，只留出处。 */
function LinkOnlyNotice({ detail }: { detail: BenchmarkDetail }) {
  return (
    <section>
      <h2 className="text-base font-semibold text-gray-900">站内只登记出处</h2>
      <p className="mt-2 text-sm text-gray-600">
        这张榜没有可对外发布的机器可读榜单，站内只登记名称与出处，不陈列版本与分数。
      </p>
      {detail.homepageUrl ? (
        <p className="mt-2 text-sm text-gray-600">
          官方页面：
          <a
            href={detail.homepageUrl}
            rel="noreferrer noopener"
            target="_blank"
            className="text-gray-900 underline-offset-2 hover:underline"
          >
            {detail.homepageUrl}
          </a>
        </p>
      ) : null}
    </section>
  );
}

export function BenchmarkDetailPage() {
  const { slug = '' } = useParams<{ slug: string }>();

  const query = useQuery({
    queryKey: ['benchmark', slug],
    queryFn: async (): Promise<BenchmarkDetail | null> => {
      const response = await getBenchmark(slug);
      if (response.status === 404) return null;
      if (response.status !== 200) throw new Error('基准详情加载失败（' + response.status + '）');
      return response.data;
    },
    enabled: slug !== '',
  });

  if (query.isPending) {
    return (
      <Page title="基准">
        <div className="mt-6">
          <Skeleton lines={6} />
        </div>
      </Page>
    );
  }

  if (query.isError) {
    return (
      <Page title="基准">
        <div className="mt-6">
          <ErrorState
            title="基准详情加载失败"
            description={query.error instanceof Error ? query.error.message : '请稍后重试'}
            onRetry={() => void query.refetch()}
            retrying={query.isFetching}
          />
        </div>
      </Page>
    );
  }

  const detail = query.data;
  if (detail === null || detail === undefined) {
    return (
      <Page title="基准">
        <div className="mt-6">
          <EmptyState
            title="这张基准不存在或已下架"
            description="它可能被改名或从目录里移除。"
            action={<Link to="/benchmarks">回基准目录</Link>}
          />
        </div>
      </Page>
    );
  }

  const thirdParty = detail.versions.filter((version) => version.sourceType !== 'self_reported');
  const selfReported = detail.versions.filter((version) => version.sourceType === 'self_reported');
  const current = latestThirdParty(detail.versions);

  return (
    <Page title={detail.name} description={detail.description ?? undefined}>
      <div className="mt-6 flex flex-col gap-8">
        <OverviewSection detail={detail} />

        {detail.displayTier === 'table' ? (
          <>
            <LatestVersionSection current={current} />
            <MetricsSection metrics={detail.metrics} />
            <VersionHistorySection thirdParty={thirdParty} selfReported={selfReported} />
          </>
        ) : detail.displayTier === 'cite' ? (
          <CiteNotice detail={detail} />
        ) : (
          <LinkOnlyNotice detail={detail} />
        )}

        <p className="m-0 text-sm">
          <Link to="/benchmarks" className="underline-offset-2 hover:underline">
            回基准目录
          </Link>
        </p>
      </div>
    </Page>
  );
}
