import { useId, useState } from 'react';

/**
 * 五颗星评分控件。
 *
 * # 为什么自己写而不是用 Ring UI
 *
 * Ring UI 里没有评分控件（`components/` 下只有 input / input-size / tags-input / text 那一类）。
 * AGENTS.md 允许「简单自定义样式用 Tailwind utilities」——五颗星正好是这个范围。
 *
 * # 为什么是按钮不是 radio
 *
 * 评分是「点一下就好」的交互，radio 需要额外的键盘模型才能表达「再点一次同一颗星」。
 * 这里用 `role="radiogroup"` + 每个按钮 `role="radio"`：既保住语义（读屏会说
 * 「第 3 颗，共 5 颗，已选中」），也让点击直接落到具体分值上。
 *
 * # 为什么支持 again-点击撤销
 *
 * 点已经选中的那颗星表示「我不想打这个分了」。没有这个动作，用户改主意的唯一办法
 * 是去找页面别处的「取消评分」——而评分区通常没有那个按钮。
 */
export interface RatingProps {
  /** 当前分数；0 表示还没打过分。 */
  value: number;
  /** 分值变化。传 0 表示撤销。 */
  onChange: (score: number) => void;
  /** 提交中：禁用交互，避免连点写出两次请求。 */
  disabled?: boolean;
  /** 无障碍名，例如「给《钢琴卷帘》打分」。 */
  label: string;
  /** 是否显示「4 分」这样的文字说明。 */
  showValueText?: boolean;
}

const SCORES = [1, 2, 3, 4, 5] as const;

const STAR = (
  <svg viewBox="0 0 20 20" aria-hidden="true" className="size-6">
    <path
      d="M10 1.6l2.6 5.3 5.9.9-4.3 4.1 1 5.8L10 14.9l-5.2 2.8 1-5.8L1.5 7.8l5.9-.9z"
      fill="currentColor"
    />
  </svg>
);

export function Rating({ value, onChange, disabled, label, showValueText }: RatingProps) {
  const id = useId();
  const [hover, setHover] = useState(0);

  // 悬停预览：鼠标经过第 4 颗星时，前面四颗都亮。移开后回到真实分值。
  const shown = hover || value;

  function pick(score: number) {
    if (disabled) return;
    // 点已选中的那颗 = 撤销。这条规则写在文档注释里，也写在 aria-label 上，
    // 否则用户永远发现不了。
    onChange(score === value ? 0 : score);
  }

  return (
    <div className="flex items-center gap-2">
      <span id={`${id}-label`} className="text-sm text-gray-600 dark:text-gray-400">
        {label}
      </span>
      <div
        role="radiogroup"
        aria-labelledby={`${id}-label`}
        aria-disabled={disabled || undefined}
        className="flex items-center gap-0.5"
        onMouseLeave={() => setHover(0)}
      >
        {SCORES.map((score) => {
          const filled = score <= shown;
          return (
            <button
              key={score}
              type="button"
              role="radio"
              aria-checked={score === value}
              aria-label={score === value ? `取消评分（当前 ${score} 分）` : `打 ${score} 分`}
              disabled={disabled}
              onMouseEnter={() => setHover(score)}
              onFocus={() => setHover(score)}
              onBlur={() => setHover(0)}
              onClick={() => pick(score)}
              className={[
                'rounded p-0.5 transition-colors',
                'focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-1',
                'focus-visible:outline-blue-500',
                disabled ? 'cursor-not-allowed opacity-50' : 'cursor-pointer',
                filled ? 'text-amber-500' : 'text-gray-300 dark:text-gray-600 hover:text-amber-400',
              ].join(' ')}
            >
              {STAR}
            </button>
          );
        })}
      </div>
      {showValueText ? (
        <span className="text-sm tabular-nums text-gray-600 dark:text-gray-400">
          {/* 「你还没评分」说的是我自己的票；页面旁边那句「还没有人评分」说的是全站。
              两句话原本都写成「未评分」，看起来像自相矛盾（issue #377）。 */}
          {value > 0 ? `${value} 分` : '你还没评分'}
        </span>
      ) : null}
    </div>
  );
}
