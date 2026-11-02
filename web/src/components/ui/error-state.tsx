import { Button } from './button';

export interface ErrorStateProps {
  /** 一句话说清发生了什么，不要贴原始错误信息。 */
  title: string;
  /** 可选的排查提示或错误码。 */
  description?: string;
  /** 有就显示「重试」按钮。 */
  onRetry?: () => void;
  /** 重试进行中。 */
  retrying?: boolean;
}

/**
 * 错误状态。
 *
 * 注意两点：
 * 1. **不要把后端错误原文直接展示**——它可能带着路径、SQL 片段甚至连接串。
 *    调用方负责翻译成人话，这个组件只负责排版。
 * 2. 错误态要有出路：能重试就给重试，不能重试就说清去哪儿。
 */
export function ErrorState({ title, description, onRetry, retrying = false }: ErrorStateProps) {
  return (
    <div role="alert" className="flex flex-col items-center justify-center gap-2 py-16 text-center">
      <p className="text-base font-medium text-red-700 dark:text-red-400">{title}</p>
      {description && (
        <p className="max-w-md text-sm text-gray-500 dark:text-gray-400">{description}</p>
      )}
      {onRetry && (
        <div className="mt-2">
          <Button disabled={retrying} onClick={onRetry}>
            {retrying ? '重试中…' : '重试'}
          </Button>
        </div>
      )}
    </div>
  );
}
