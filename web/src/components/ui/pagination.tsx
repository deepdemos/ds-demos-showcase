import { Button } from './button';

export interface PaginationProps {
  /** 当前页，从 1 开始。 */
  page: number;
  /** 总页数。小于 1 时组件不渲染。 */
  pageCount: number;
  /** 页码变化。只在 1..pageCount 范围内触发。 */
  onPageChange: (page: number) => void;
  /** 整组禁用（例如正在加载）。 */
  disabled?: boolean;
}

/**
 * 分页器。只做一件事：在 1..pageCount 之间选一个页码。
 *
 * 为什么不显示全部页码：作品库未来会有上百页，全部铺开会把工具栏撑爆。这里用
 * 「首 / 上一页 / 当前 x/y / 下一页 / 末」五个位置，页码本身用文本表达。
 */
export function Pagination({ page, pageCount, onPageChange, disabled = false }: PaginationProps) {
  if (pageCount < 1) return null;

  const go = (next: number) => {
    const clamped = Math.min(Math.max(next, 1), pageCount);
    if (clamped !== page) onPageChange(clamped);
  };

  return (
    <nav aria-label="分页" className="flex items-center justify-center gap-2 py-4">
      <Button disabled={disabled || page <= 1} onClick={() => go(1)}>
        首页
      </Button>
      <Button disabled={disabled || page <= 1} onClick={() => go(page - 1)}>
        上一页
      </Button>
      <span
        className="px-2 text-sm text-gray-600 tabular-nums dark:text-gray-400"
        aria-live="polite"
      >
        第 {page} / {pageCount} 页
      </span>
      <Button disabled={disabled || page >= pageCount} onClick={() => go(page + 1)}>
        下一页
      </Button>
      <Button disabled={disabled || page >= pageCount} onClick={() => go(pageCount)}>
        末页
      </Button>
    </nav>
  );
}
