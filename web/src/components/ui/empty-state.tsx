import type { ReactNode } from 'react';

export interface EmptyStateProps {
  /** 一句话说清「这里为什么是空的」。不要写成「暂无数据」。 */
  title: string;
  /** 可选的补充说明：怎么才会有数据。 */
  description?: string;
  /** 可选的操作，例如「去发布」。 */
  action?: ReactNode;
}

/**
 * 空状态。区分「还没有内容」和「筛选后没有结果」是调用方的事——
 * 这个组件只负责把这句话说清楚，以及给一个下一步动作的位置。
 */
export function EmptyState({ title, description, action }: EmptyStateProps) {
  return (
    <div className="flex flex-col items-center justify-center gap-2 py-16 text-center">
      <p className="text-base font-medium text-gray-700 dark:text-gray-200">{title}</p>
      {description && (
        <p className="max-w-md text-sm text-gray-500 dark:text-gray-400">{description}</p>
      )}
      {action && <div className="mt-2">{action}</div>}
    </div>
  );
}
