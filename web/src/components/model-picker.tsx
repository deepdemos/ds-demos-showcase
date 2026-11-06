import { useQuery } from '@tanstack/react-query';
import { listModels, type Model } from '@/api';
import { Button, Checkbox } from '@/components/ui';

// 模型声明是「作者自己说用了哪些模型」。选项目录来自后端 `GET /api/v1/models`，
// 不写死在前端：写死一份就要在新增模型时发一次前端版本，而且那份迟早与库里不一致。
//
// 为什么是勾选而不是自由输入：模型是主数据（有 slug、有状态），自由输入立刻长出
// 「deepseek-chat」「DeepSeek Chat」「deepseek chat v3」三种写法，之后没法聚合。
// 只取 `active` 的：目录端点现在默认返回全部状态（候选 / 未核实 / 已退役），
// 那是给目录页用的。投稿表单让人**声明**一个模型，候选与已退役的都不该出现在
// 选择题里——前者还没被核实，后者已经并到别处了。筛选责任在调用方，正是这个意思。
async function fetchModels(): Promise<Model[]> {
  const response = await listModels({ status: 'active' });
  if (response.status === 200) return response.data.items;
  throw new Error(`模型目录加载失败（HTTP ${response.status}）`);
}

export type ModelPickerProps = {
  /** 已选中的模型 slug。 */
  value: string[];
  onChange: (next: string[]) => void;
  disabled?: boolean;
  /** 目录里有声明但已不在目录中的 slug（已退役 / 合并），只读展示，别让人以为丢了。 */
  extra?: string[];
};

export function ModelPicker({ value, onChange, disabled, extra = [] }: ModelPickerProps) {
  const { data, isPending, isError, refetch, isFetching } = useQuery({
    queryKey: ['models'],
    queryFn: fetchModels,
    staleTime: 5 * 60 * 1000,
  });

  const toggle = (slug: string, checked: boolean) => {
    onChange(
      checked ? [...value.filter((s) => s !== slug), slug] : value.filter((s) => s !== slug),
    );
  };

  // 目录读不到不该挡住投稿：声明是可选项，作者可以稍后在编辑页补。
  if (isError) {
    return (
      <div className="flex flex-col gap-1 text-sm">
        <span>模型声明（可留空）</span>
        <span className="text-xs text-gray-500 dark:text-gray-400">
          模型目录暂时读不到，可以先不声明，稍后在编辑页补。
        </span>
        <span className="self-start">
          <Button type="button" inline onClick={() => void refetch()} disabled={isFetching}>
            {isFetching ? '重试中…' : '重试'}
          </Button>
        </span>
      </div>
    );
  }

  if (isPending) {
    return (
      <div className="flex flex-col gap-1 text-sm">
        <span>模型声明（可留空）</span>
        <span className="text-xs text-gray-500 dark:text-gray-400">正在读取模型目录…</span>
      </div>
    );
  }

  // 目录为空是合法的：主数据还没导入。这时说清楚，别让作者以为是自己选错了。
  if (data.length === 0 && extra.length === 0) {
    return (
      <div className="flex flex-col gap-1 text-sm">
        <span>模型声明（可留空）</span>
        <span className="text-xs text-gray-500 dark:text-gray-400">
          模型目录还没有内容，暂时无法声明。
        </span>
      </div>
    );
  }

  // 已选但不在目录里的旧声明：显示出来、允许取消勾选，否则作者看不到也删不掉。
  const knownSlugs = new Set(data.map((m) => m.slug));
  const orphans = extra.filter((slug) => !knownSlugs.has(slug));

  return (
    <fieldset className="flex flex-col gap-2 text-sm">
      <legend className="mb-1">模型声明（可留空，可多选）</legend>
      <span className="text-xs text-gray-500 dark:text-gray-400">
        只列已启用的模型。这是作者自己的声明，不是平台核验过的结论。
      </span>
      {data.map((model) => (
        <Checkbox
          key={model.slug}
          checked={value.includes(model.slug)}
          onChange={(e) => toggle(model.slug, e.currentTarget.checked)}
          disabled={disabled}
          indeterminate={false}
          label={model.vendor ? `${model.name}（${model.vendor}）` : model.name}
        />
      ))}
      {orphans.map((slug) => (
        <Checkbox
          key={slug}
          checked={value.includes(slug)}
          onChange={(e) => toggle(slug, e.currentTarget.checked)}
          disabled={disabled}
          indeterminate={false}
          label={`${slug}（已不在模型目录中）`}
        />
      ))}
    </fieldset>
  );
}
