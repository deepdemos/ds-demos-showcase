import { useId, type TextareaHTMLAttributes } from 'react';

/**
 * 多行文本输入。
 *
 * Ring UI 没有 textarea（只有 input / tags-input），所以这里按 AGENTS 里「简单自定义样式
 * 用 Tailwind utilities」自己写一个，把三处页面里各抄一遍的类名收成一处。
 *
 * 错误态不靠调用方传 className 拼：`error` 同时负责**视觉**（红边框）与**语义**
 * （`aria-invalid` + `aria-describedby` 指向错误文本）。只变颜色的话，读屏用户
 * 听不出哪一栏填错了。
 */
export type TextareaProps = Omit<TextareaHTMLAttributes<HTMLTextAreaElement>, 'rows'> & {
  /** 错误提示。给了就进错误态，并把提示挂到 `aria-describedby` 上。 */
  error?: string;
  /** 行数，默认 4。 */
  rows?: number;
};

export function Textarea({ error, rows = 4, className = '', ...rest }: TextareaProps) {
  const errorId = useId();
  const border = error
    ? 'border-red-500 dark:border-red-500'
    : 'border-gray-300 dark:border-gray-700';

  return (
    <span className="flex w-full flex-col gap-1">
      <textarea
        {...rest}
        rows={rows}
        aria-invalid={error ? true : undefined}
        aria-describedby={error ? errorId : undefined}
        className={`w-full rounded-lg border bg-white px-3 py-2 text-sm text-gray-800 dark:bg-gray-900 dark:text-gray-100 ${border} ${className}`}
      />
      {error ? (
        <span id={errorId} role="alert" className="text-xs text-red-600 dark:text-red-400">
          {error}
        </span>
      ) : null}
    </span>
  );
}
