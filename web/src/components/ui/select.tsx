export interface SelectOption {
  value: string;
  /** 给用户看的文案 */
  label: string;
  disabled?: boolean;
}

export interface SelectProps {
  /** 当前值 */
  value: string;
  options: SelectOption[];
  onChange: (value: string) => void;
  disabled?: boolean;
  /** 可访问名。没有可见 `<label>` 时必填，否则读屏软件只会念「组合框」。 */
  ariaLabel?: string;
  id?: string;
  /** 校验失败时的错误文案。 */
  error?: string;
  /** 没有选中值时显示的占位项。 */
  placeholder?: string;
}

/**
 * 下拉选择。
 *
 * 为什么不用 Ring UI 的 Select：那个组件的值是数据驱动的，一行选项要包成对象，
 * 而本仓库的选择项全部来自静态枚举或接口返回的短列表，原生 `<select>` 加
 * Tailwind 就够。控件库存在的意义是「页面只有一个控件来源」，不是「控件越重越好」。
 */
export function Select({
  value,
  options,
  onChange,
  disabled,
  ariaLabel,
  id,
  error,
  placeholder,
}: SelectProps) {
  return (
    <div className="flex flex-col gap-1">
      <select
        id={id}
        value={value}
        disabled={disabled}
        aria-label={ariaLabel}
        aria-invalid={error ? true : undefined}
        onChange={(event) => onChange(event.target.value)}
        className="w-full rounded-lg border border-gray-300 bg-white px-3 py-2 text-sm text-gray-800 disabled:cursor-not-allowed disabled:opacity-60 dark:border-gray-700 dark:bg-gray-900 dark:text-gray-100"
      >
        {placeholder !== undefined && (
          <option value="" disabled>
            {placeholder}
          </option>
        )}
        {options.map((option) => (
          <option key={option.value} value={option.value} disabled={option.disabled}>
            {option.label}
          </option>
        ))}
      </select>
      {error && (
        <span className="text-xs text-red-600 dark:text-red-400" role="alert">
          {error}
        </span>
      )}
    </div>
  );
}
