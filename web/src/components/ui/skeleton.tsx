export interface SkeletonProps {
  /** 要画几行占位条。默认 3。 */
  lines?: number;
  /** 单行高度，用 Tailwind 的 h-* 类表达，例如 `h-4`。 */
  lineHeight?: string;
}

/**
 * 骨架屏。只在「首屏还没拿到数据」时用；已经有数据再刷新时不要换成骨架，
 * 那会让页面闪一下，比不动更难受（列表页用 isFetching 做淡出更合适）。
 */
export function Skeleton({ lines = 3, lineHeight = 'h-4' }: SkeletonProps) {
  return (
    <div aria-hidden className="flex flex-col gap-3 py-4">
      {Array.from({ length: lines }, (_, i) => (
        <div
          key={i}
          className={`animate-pulse rounded bg-gray-200 ${lineHeight} dark:bg-gray-700 ${
            i === lines - 1 ? 'w-3/5' : 'w-full'
          }`}
        />
      ))}
    </div>
  );
}
