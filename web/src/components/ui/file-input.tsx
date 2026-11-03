import type { ChangeEvent, Ref } from 'react';

export interface FileInputProps {
  /** 选中文件的提示文案。用「选择一个 zip 包」这样的具体说法，不要写「上传文件」。 */
  label?: string;
  /** 允许的文件类型，直接透传给原生 `accept`（如 `.zip,application/zip`）。 */
  accept?: string;
  /** 选中后的文件；受控由调用方持有，便于提交时直接拿。 */
  file?: File | null;
  /** 选择变化回调。清空时给 `null`。 */
  onFileChange: (file: File | null) => void;
  disabled?: boolean;
  /** 校验失败时的错误文案。 */
  error?: string;
  /** 允许的大小上限（字节）。超过时不触发回调，只调用 `onReject`。 */
  maxBytes?: number;
  /** 被拒时调用，参数是给用户看的中文原因。 */
  onReject?: (reason: string) => void;
  ref?: Ref<HTMLInputElement>;
}

function humanSize(bytes: number): string {
  if (bytes >= 1024 * 1024) return `${Math.round(bytes / (1024 * 1024))} MB`;
  if (bytes >= 1024) return `${Math.round(bytes / 1024)} KB`;
  return `${bytes} 字节`;
}

/**
 * 文件选择。
 *
 * 为什么包一层原生 `input[type=file]` 而不是直接用：页面只能从控件库取控件，
 * 而「选中了什么、超没超限、清空」这套行为每个调用方都要重写一遍。
 * 大小上限在这里判，是为了让用户在**选完之后立刻**知道，而不是等上传到一半。
 *
 * 注意：这里判上限只是体验优化，服务端仍会拿真实请求体再判一次——
 * 前端能改的东西都不能当安全边界。
 */
export function FileInput({
  label = '选择文件',
  accept,
  file,
  onFileChange,
  disabled,
  error,
  maxBytes,
  onReject,
  ref,
}: FileInputProps) {
  const onChange = (event: ChangeEvent<HTMLInputElement>) => {
    const picked = event.target.files?.[0] ?? null;
    if (picked && maxBytes !== undefined && picked.size > maxBytes) {
      onReject?.(`文件 ${humanSize(picked.size)}，超过上限 ${humanSize(maxBytes)}`);
      // 清掉原生控件的选择，否则再次选同一个文件不会触发 change
      event.target.value = '';
      onFileChange(null);
      return;
    }
    onFileChange(picked);
  };

  return (
    <div className="flex flex-col gap-1">
      <input
        ref={ref}
        type="file"
        accept={accept}
        disabled={disabled}
        onChange={onChange}
        className="block w-full cursor-pointer rounded-lg border border-gray-300 bg-white px-3 py-2 text-sm text-gray-700 file:mr-3 file:rounded file:border-0 file:bg-gray-100 file:px-3 file:py-1 file:text-sm disabled:cursor-not-allowed disabled:opacity-60 dark:border-gray-700 dark:bg-gray-900 dark:text-gray-200 dark:file:bg-gray-800"
      />
      <span className="text-xs text-gray-500 dark:text-gray-400">
        {file ? `${label}：${file.name}（${humanSize(file.size)}）` : label}
      </span>
      {error && (
        <span className="text-xs text-red-600 dark:text-red-400" role="alert">
          {error}
        </span>
      )}
    </div>
  );
}
