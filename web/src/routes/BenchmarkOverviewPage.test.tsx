import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { BenchmarkOverviewPage } from './BenchmarkOverviewPage';

const getBenchmarkOverview = vi.hoisted(() => vi.fn());

vi.mock('@/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/api')>();
  return { ...actual, getBenchmarkOverview };
});

/**
 * 服务端顺序（#572）：列 coding → agent → general，组内按榜名；行按有格子的列数降序。
 *
 * 两处「故意与前端直觉相反」，用来验页面没有偷偷重排：
 * - 列：编码组有两个人（HumanEval、SWE-bench），组标题要跨两列；
 * - 行：DeepSeek Reasoner 覆盖 3 列排第一，而按模型名排 DeepSeek Coder 会在前面。
 */
const overview = {
  columns: [
    { benchmarkSlug: 'humaneval', name: 'HumanEval', category: 'coding', unit: '%', version: 'v1' },
    { benchmarkSlug: 'swebench', name: 'SWE-bench', category: 'coding', unit: '%', version: 'v1' },
    { benchmarkSlug: 'gaia', name: 'GAIA', category: 'agent', unit: '%', version: 'v2' },
    { benchmarkSlug: 'mmlu', name: 'MMLU', category: 'general', unit: '%', version: '2026-11' },
  ],
  rows: [
    {
      modelSlug: 'deepseek-reasoner',
      modelName: 'DeepSeek Reasoner',
      cells: [
        { benchmarkSlug: 'humaneval', rawScore: 84.1, rank: null },
        { benchmarkSlug: 'gaia', rawScore: 61.5, rank: 2 },
        { benchmarkSlug: 'mmlu', rawScore: 81.2, rank: 1 },
      ],
    },
    {
      modelSlug: 'deepseek-coder',
      modelName: 'DeepSeek Coder',
      cells: [{ benchmarkSlug: 'humaneval', rawScore: 86.4, rank: 1 }],
    },
  ],
  totalColumns: 4,
  totalRows: 2,
};

function renderPage() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter>
        <BenchmarkOverviewPage />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  getBenchmarkOverview.mockReset();
});

describe('BenchmarkOverviewPage', () => {
  it('一行一个模型、一列一张榜，格子给原始分与名次', async () => {
    getBenchmarkOverview.mockResolvedValue({ status: 200, data: overview });
    renderPage();

    expect(await screen.findByRole('rowheader', { name: 'DeepSeek Coder' })).toBeInTheDocument();
    expect(screen.getByRole('rowheader', { name: 'DeepSeek Reasoner' })).toBeInTheDocument();

    // 列头：榜名 + 单位 · 版本；点进去是该榜详情。
    const header = screen.getByRole('link', { name: 'HumanEval' });
    expect(header).toHaveAttribute('href', '/benchmarks/humaneval');
    expect(screen.getByRole('columnheader', { name: /HumanEval/ })).toHaveTextContent('% · v1');

    const row = screen.getByRole('rowheader', { name: 'DeepSeek Coder' }).closest('tr');
    const cells = within(row as HTMLElement).getAllByRole('cell');
    expect(cells[0]).toHaveTextContent('86.4');
    expect(cells[0]).toHaveTextContent('#1');
    // 没有超限就不该有截断提示。
    expect(screen.queryByRole('status')).toBeNull();
  });

  it('缺格显示「—」，不补零', async () => {
    getBenchmarkOverview.mockResolvedValue({ status: 200, data: overview });
    renderPage();

    // DeepSeek Coder 只在 HumanEval 一列有分数：其余三列都是缺格。
    const row = (await screen.findByRole('rowheader', { name: 'DeepSeek Coder' })).closest('tr');
    const cells = within(row as HTMLElement).getAllByRole('cell');
    for (const cell of cells.slice(1)) {
      expect(cell).toHaveTextContent('—');
      expect(cell).not.toHaveTextContent('0');
    }
  });

  it('源榜没给名次时只显示分数，不编一个名次', async () => {
    getBenchmarkOverview.mockResolvedValue({ status: 200, data: overview });
    renderPage();

    const row = (await screen.findByRole('rowheader', { name: 'DeepSeek Reasoner' })).closest('tr');
    const cells = within(row as HTMLElement).getAllByRole('cell');
    expect(cells[0]).toHaveTextContent('84.1');
    expect(cells[0]).not.toHaveTextContent('#');
  });

  it('一列都没有时给空态，不白屏也不编总分', async () => {
    getBenchmarkOverview.mockResolvedValue({
      status: 200,
      data: { columns: [], rows: [], totalColumns: 0, totalRows: 0 },
    });
    renderPage();

    expect(await screen.findByText('还没有能放进矩阵的分数')).toBeInTheDocument();
    expect(screen.queryByText(/综合分|总分/)).toBeNull();
  });

  it('列按能力象限分组，组标题占一整行', async () => {
    getBenchmarkOverview.mockResolvedValue({ status: 200, data: overview });
    renderPage();

    await screen.findByRole('rowheader', { name: 'DeepSeek Coder' });
    const headerRows = within(screen.getByRole('table')).getAllByRole('row');
    const groupRow = headerRows[0];
    const groups = within(groupRow).getAllByRole('columnheader');
    expect(groups.map((header) => header.textContent)).toEqual([
      '模型',
      '编码与软件工程',
      '工具与 agent',
      '通用与综合',
    ]);

    // 组标题是 colspan 的真表格结构，不是另起一张表。
    expect(groups[1]).toHaveAttribute('scope', 'colgroup');
    expect(groups[1]).toHaveAttribute('colspan', '2');
    expect(groups[2]).toHaveAttribute('colspan', '1');

    // 第二行才是各榜列头，顺序与响应一致（组内按榜名，前端不重排）。
    const columnRow = within(headerRows[1]).getAllByRole('columnheader');
    expect(columnRow.map((header) => header.textContent)).toEqual([
      'HumanEval% · v1',
      'SWE-bench% · v1',
      'GAIA% · v2',
      'MMLU% · 2026-11',
    ]);
  });

  it('行按服务端给的覆盖数顺序渲染，前端不再重排', async () => {
    getBenchmarkOverview.mockResolvedValue({ status: 200, data: overview });
    renderPage();

    await screen.findByRole('rowheader', { name: 'DeepSeek Coder' });
    const names = screen.getAllByRole('rowheader').map((row) => row.textContent);
    // 按模型名排会得到 Coder → Reasoner；服务端给的是 Reasoner（3 格）在前。
    expect(names).toEqual(['DeepSeek Reasoner', 'DeepSeek Coder']);
  });

  it('超过行列上限时明确写只显示前 N 列 / 前 N 行', async () => {
    getBenchmarkOverview.mockResolvedValue({
      status: 200,
      data: { ...overview, totalColumns: 26, totalRows: 201 },
    });
    renderPage();

    const notice = await screen.findByRole('status');
    expect(notice).toHaveTextContent('列只显示前 4 列（共 26 列）');
    expect(notice).toHaveTextContent('行只显示前 2 行（共 201 行）');
  });

  it('表头带 scope，首列与表头吸顶，窄屏横向滚动', async () => {
    getBenchmarkOverview.mockResolvedValue({ status: 200, data: overview });
    renderPage();

    await screen.findByRole('rowheader', { name: 'DeepSeek Coder' });
    for (const header of screen.getAllByRole('columnheader')) {
      expect(header).toHaveAttribute('scope');
    }
    for (const row of screen.getAllByRole('rowheader')) {
      expect(row).toHaveAttribute('scope', 'row');
    }

    // 首列与表头 sticky：这两个 class 是吸顶能成立的前提，jsdom 没有布局，只能查标记。
    const modelHeader = screen.getByRole('columnheader', { name: '模型' });
    expect(modelHeader.className).toContain('sticky');
    expect(modelHeader.className).toContain('left-0');
    expect(modelHeader.className).toContain('top-0');
    const benchmarkHeader = screen.getByRole('columnheader', { name: /HumanEval/ });
    expect(benchmarkHeader.className).toContain('sticky');
    expect(benchmarkHeader.className).toContain('top-0');
    const rowHeader = screen.getByRole('rowheader', { name: 'DeepSeek Coder' });
    expect(rowHeader.className).toContain('sticky');
    expect(rowHeader.className).toContain('left-0');

    // 窄屏横向滚动靠外层滚动容器 + 表格最小宽度。
    const table = screen.getByRole('table');
    expect(table.className).toContain('min-w-[48rem]');
    expect(table.parentElement?.className).toContain('overflow-auto');
  });
});
