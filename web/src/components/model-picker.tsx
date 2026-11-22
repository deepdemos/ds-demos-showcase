import { useQuery } from '@tanstack/react-query';
import { listModels, type Model } from '@/api';
import { Button, Checkbox } from '@/components/ui';

// 模型声明是「作者自己说用了哪些模型」。目录来自后端 `GET /api/v1/models`，
// 不写死在前端：写死一份就要在新增模型时发一次前端版本，而且那份迟早与库里不一致。
//
// 目录就是可声明范围
//
// 2026-11-06 曾有过「目录里没有也能手填，服务端解析不到就建一条 candidate」，
// 2026-11-17 撤掉（#296 → 撤销见 #465）。理由不是「灰测模型不该能声明」，
// 而是那条路把模型表变成了任何登录用户都能写的表：脏名字只能靠人工归并消化，
// 而归并端点至今没有；需求 MODEL-001~021 里也没有「作者可以造模型名」这一条。
// 灰测模型要能声明，走的是「申请收录 → 管理员审核进目录」，不是让作者现编一个。
//
// 只把 `active` 的放进勾选列表：目录端点默认返回全部状态（候选 / 未核验 / 已退役），
// 那是给目录页用的。这里让人从已确认的模型里挑，候选与已退役不该混在选择题里。
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
  /**
   * 作品上已有、但不在可选目录里的 slug（已退役 / 已合并 / 还没核验）。
   *
   * 不给它的话，预填时这些 slug 既不在勾选列表里、也不在 value 里，
   * 保存一次就被悄悄清掉——作者看不出发生过什么。
   */
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

  if (isPending) {
    return (
      <div className="flex flex-col gap-1 text-sm">
        <span>模型声明（可留空）</span>
        <span className="text-xs text-gray-500">正在读取模型目录…</span>
      </div>
    );
  }

  // 目录读不到时**不挡投稿**：声明是可选项，读不到就当作这次不声明。
  // 挡住的代价是作者上传一个大包之后才发现提交不了。
  if (isError || !data) {
    return (
      <div className="flex flex-col gap-1 text-sm">
        <span>模型声明（可留空）</span>
        <span className="text-xs text-gray-500">
          模型目录暂时读不到，可以这次先不声明，稍后在编辑页补。
        </span>
        <SelectedList value={value} onRemove={(s) => toggle(s, false)} disabled={disabled} />
        <span className="self-start">
          <Button type="button" inline onClick={() => void refetch()} disabled={isFetching}>
            {isFetching ? '重试中…' : '重试'}
          </Button>
        </span>
      </div>
    );
  }

  // 目录为空是合法的：主数据还没导入。此时没有可选项，也不给手填入口。
  if (data.length === 0 && value.length === 0 && extra.length === 0) {
    return (
      <div className="flex flex-col gap-1 text-sm">
        <span>模型声明（可留空）</span>
        <span className="text-xs text-gray-500">模型目录还没有内容，暂时无法声明模型。</span>
      </div>
    );
  }

  const known = new Set(data.map((m) => m.slug));
  // 目录里没有、但作品上带着的：仍要显示出来，否则作者既看不到也删不掉。
  // 含早先手填留下的名字：它们不在可选目录里，但仍是可勾选的——取消即从声明里去掉，
  // 勾回来就重新提交（服务端仍能解析这些 slug，只是不再新建）。
  const orphans = [...new Set([...extra, ...value])].filter((s) => !known.has(s));

  return (
    <fieldset className="flex flex-col gap-2 text-sm">
      <legend className="mb-1">模型声明（可留空，可多选）</legend>
      <span className="text-xs text-gray-500">
        勾选目录里的模型。这是作者自己的声明，不是平台核验过的结论。
      </span>
      {data.length === 0 ? (
        <span className="text-xs text-gray-500">模型目录还没有内容。</span>
      ) : (
        data.map((model) => (
          <Checkbox
            key={model.slug}
            checked={value.includes(model.slug)}
            onChange={(e) => toggle(model.slug, e.currentTarget.checked)}
            disabled={disabled}
            indeterminate={false}
            label={model.vendor ? `${model.name}（${model.vendor}）` : model.name}
          />
        ))
      )}
      {orphans.length > 0 ? (
        <div className="flex flex-col gap-1">
          <span className="text-xs text-gray-600">
            作品上还声明了这些模型，但当前可选目录里没有（可能已退役、已合并或还没核验）：
          </span>
          {orphans.map((slug) => (
            <Checkbox
              key={slug}
              checked={value.includes(slug)}
              onChange={(e) => toggle(slug, e.currentTarget.checked)}
              disabled={disabled}
              indeterminate={false}
              label={`${slug}（不在可选目录中）`}
            />
          ))}
        </div>
      ) : null}
      <span className="text-xs text-gray-500">
        目录里没有的模型现在声明不了——需要先收录进目录，缺了哪一条可以告诉管理员。
      </span>
    </fieldset>
  );
}

/** SelectedList 用于目录读不到时仍要能看到「已经声明的有哪些」。 */
function SelectedList({
  value,
  onRemove,
  disabled,
}: {
  value: string[];
  onRemove: (ref: string) => void;
  disabled?: boolean;
}) {
  if (value.length === 0) return null;
  return (
    <ul className="flex flex-col gap-1 text-xs">
      {value.map((ref) => (
        <li key={ref} className="flex items-center gap-2">
          <span>{ref}</span>
          <Button type="button" inline onClick={() => onRemove(ref)} disabled={disabled}>
            移除
          </Button>
        </li>
      ))}
    </ul>
  );
}
