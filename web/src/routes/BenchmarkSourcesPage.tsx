import { Link } from 'react-router';
import { Page } from '@/components/layout/page';
import {
  benchmarkSources,
  groupSourcesByCategory,
  type BenchmarkSource,
} from '@/content/benchmark-sources';
import { tierLabel } from '@/content/benchmark-tier';

/**
 * 基准来源（`/benchmarks/sources`，issue #568）。
 *
 * 回答「业界这些榜是什么、靠不靠谱、我们收不收」，**不回答「谁最强」**。
 * 内容来自 `src/content/benchmark-sources.json`：改文案改 JSON、走 PR。
 *
 * 三条展示口径：
 * - 成本缺失写「未公布」——不换算、不估算。
 * - 内部证据标记（A/B/未核）不上对外页。
 * - 未收录的榜也列出来，并在「档位」里说明为什么。
 */

function SourceRows({ item }: { item: BenchmarkSource }) {
  return (
    <tr className="border-b border-gray-100 align-top">
      <td className="py-3 pr-4">
        <a
          href={item.url}
          target="_blank"
          rel="noreferrer noopener"
          className="text-gray-900 underline-offset-2 hover:underline"
        >
          {item.name}
        </a>
        <div className="mt-0.5 text-xs text-gray-500">{item.publisher}</div>
        {item.notes.map((note) => (
          <div key={note} className="mt-1 text-xs text-gray-500">
            ※ {note}
          </div>
        ))}
      </td>
      <td className="py-3 pr-4 text-gray-600">{item.categories.join(' · ')}</td>
      <td className="py-3 pr-4 text-gray-600">{item.contamination}</td>
      <td className="py-3 pr-4 text-gray-600">{item.harness}</td>
      <td className="py-3 pr-4 text-gray-600">{item.metricCost || '未公布'}</td>
      <td className="py-3 pr-4 text-gray-600">{item.cadence}</td>
      <td className="py-3">
        <span className="inline-block whitespace-nowrap rounded border border-gray-200 px-2 py-0.5 text-xs text-gray-600">
          {tierLabel[item.displayTier]}
        </span>
      </td>
    </tr>
  );
}

export function BenchmarkSourcesPage() {
  const groups = groupSourcesByCategory(benchmarkSources.sources);

  return (
    <Page title={benchmarkSources.title} description={benchmarkSources.intro}>
      <div className="mt-6 flex flex-col gap-8">
        <section>
          <h2 className="text-base font-semibold text-gray-900">档位</h2>
          <ul className="mt-3 flex list-none flex-col gap-1 p-0 text-sm text-gray-600">
            {benchmarkSources.tierLegend.map((tier) => (
              <li key={tier.value}>
                <span className="text-gray-900">{tierLabel[tier.value]}</span>：{tier.explain}
              </li>
            ))}
          </ul>
        </section>

        <section>
          <h2 className="text-base font-semibold text-gray-900">每列在说什么</h2>
          <dl className="mt-3 grid grid-cols-1 gap-x-6 gap-y-3 text-sm sm:grid-cols-2">
            {benchmarkSources.columns.map((column) => (
              <div key={column.key}>
                <dt className="text-gray-900">{column.label}</dt>
                <dd className="m-0 mt-0.5 text-gray-600">{column.explain}</dd>
              </div>
            ))}
          </dl>
        </section>

        {groups.map((group) => (
          <section key={group.category}>
            <h2 className="text-base font-semibold text-gray-900">
              {group.label}
              <span className="ml-2 text-sm font-normal text-gray-500">{group.items.length}</span>
            </h2>
            <div className="mt-3 overflow-x-auto">
              <table className="w-full min-w-[64rem] border-collapse text-sm">
                <thead>
                  <tr className="border-b border-gray-200 text-left text-xs text-gray-500">
                    <th scope="col" className="py-2 pr-4 font-medium">
                      评测
                    </th>
                    <th scope="col" className="py-2 pr-4 font-medium">
                      能力象限
                    </th>
                    <th scope="col" className="py-2 pr-4 font-medium">
                      防污染
                    </th>
                    <th scope="col" className="py-2 pr-4 font-medium">
                      Harness
                    </th>
                    <th scope="col" className="py-2 pr-4 font-medium">
                      指标与效费比
                    </th>
                    <th scope="col" className="py-2 pr-4 font-medium">
                      更新与维护
                    </th>
                    <th scope="col" className="py-2 font-medium">
                      档位
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {group.items.map((item) => (
                    <SourceRows key={item.id} item={item} />
                  ))}
                </tbody>
              </table>
            </div>
          </section>
        ))}

        <p className="m-0 text-xs text-gray-500">
          数据最后核实 {benchmarkSources.updatedAt}。成本缺失一律写「未公布」，站内不换算、不估算。
        </p>

        <p className="m-0 text-sm">
          <Link to="/benchmarks" className="underline-offset-2 hover:underline">
            回基准目录
          </Link>
        </p>
      </div>
    </Page>
  );
}
