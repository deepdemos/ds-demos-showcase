import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { listModels, type Model } from '@/api';
import { Button, Checkbox, Input } from '@/components/ui';

// 模型声明是「作者自己说用了哪些模型」。目录来自后端 `GET /api/v1/models`，
// 不写死在前端：写死一份就要在新增模型时发一次前端版本，而且那份迟早与库里不一致。
//
// **目录只是候选，不是白名单**：AI 圈的灰测模型有一大半上游还没收录，写死在目录里
// 等于让作者在「声明不了」和「瞎选一个」之间二选一。所以既能勾选也能手填——
// 手填的指称由服务端解析，解析不到就建一条 candidate 模型（见领域层
// `resolveModelRefs` 的注释），管理员事后归并。
//
// 只把 `active` 的放进勾选列表：目录端点默认返回全部状态（候选 / 未核实 / 已退役），
// 那是给目录页用的。这里让人**从已确认的模型里挑**，候选与已退役不该混在选择题里。
// 筛选责任在调用方，正是这个意思。
async function fetchModels(): Promise<Model[]> {
  const response = await listModels({ status: 'active' });
  if (response.status === 200) return response.data.items;
  throw new Error(`模型目录加载失败（HTTP ${response.status}）`);
}

export type ModelPickerProps = {
  /** 已选中的模型（slug，或作者手填的指称）。 */
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
  // 手填输入框的草稿。单独存是因为它只在「回车 / 点添加」时才进 value：
  // 边打边进 value 会让每敲一个字都发一次请求、也让「还没打完」被当成声明。
  const [draft, setDraft] = useState('');

  const toggle = (slug: string, checked: boolean) => {
    onChange(
      checked ? [...value.filter((s) => s !== slug), slug] : value.filter((s) => s !== slug),
    );
  };

  const addDraft = () => {
    const name = draft.trim();
    if (name === '') return;
    // 已经在列表里的（勾选过、或手填过同一个名字）不重复添加：
    // 重复的声明会让详情页出现两遍，虽然服务端会去重，界面上不该先乱一次。
    if (value.some((s) => s.toLowerCase() === name.toLowerCase())) {
      setDraft('');
      return;
    }
    onChange([...value, name]);
    setDraft('');
  };

  // 目录读不到不该挡住投稿：声明是可选项，手填仍然可用。
  // 早先这里只给一句「可以先不声明」——那等于把「目录挂了」变成作者的损失。
  if (isError) {
    return (
      <div className="flex flex-col gap-1 text-sm">
        <span>模型声明（可留空）</span>
        <span className="text-xs text-gray-500 dark:text-gray-400">
          模型目录暂时读不到，可以直接手填模型名。
        </span>
        <DraftInput draft={draft} setDraft={setDraft} onAdd={addDraft} disabled={disabled} />
        <SelectedList value={value} onRemove={(s) => toggle(s, false)} disabled={disabled} />
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

  // 目录为空是合法的：主数据还没导入。手填照样能用——这正是「目录不是白名单」的场景。
  if (data.length === 0 && extra.length === 0 && value.length === 0) {
    return (
      <div className="flex flex-col gap-1 text-sm">
        <span>模型声明（可留空）</span>
        <span className="text-xs text-gray-500 dark:text-gray-400">
          模型目录还没有内容，可以直接手填模型名。
        </span>
        <DraftInput draft={draft} setDraft={setDraft} onAdd={addDraft} disabled={disabled} />
      </div>
    );
  }

  // 已选但不在目录里的旧声明：显示出来、允许取消勾选，否则作者看不到也删不掉。
  const knownSlugs = new Set(data.map((m) => m.slug));
  const orphans = extra.filter((slug) => !knownSlugs.has(slug));

  // 手填过的名字单独列一段：既不在目录里、也不在「已退役」清单里的，
  // 就是作者自己打进去的。它们要能看见、能取消——否则打错一个字就再也去不掉。
  const orphanSet = new Set(orphans);
  const typed = value.filter((s) => !knownSlugs.has(s) && !orphanSet.has(s));

  return (
    <fieldset className="flex flex-col gap-2 text-sm">
      <legend className="mb-1">模型声明（可留空，可多选）</legend>
      <span className="text-xs text-gray-500 dark:text-gray-400">
        勾选已有的，或直接手填模型名。这是作者自己的声明，不是平台核验过的结论。
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
      {typed.map((name) => (
        <Checkbox
          key={name}
          checked
          onChange={() => toggle(name, false)}
          disabled={disabled}
          indeterminate={false}
          label={`${name}（自己填的，目录里没有）`}
        />
      ))}
      <DraftInput draft={draft} setDraft={setDraft} onAdd={addDraft} disabled={disabled} />
    </fieldset>
  );
}

/** DraftInput 是「手填一个模型名」的那一行。 */
function DraftInput({
  draft,
  setDraft,
  onAdd,
  disabled,
}: {
  draft: string;
  setDraft: (v: string) => void;
  onAdd: () => void;
  disabled?: boolean;
}) {
  return (
    <div className="flex items-end gap-2">
      <label className="flex flex-1 flex-col gap-1">
        <span className="text-xs text-gray-500 dark:text-gray-400">
          手填模型名（目录里没有的也可以，例如灰测模型）
        </span>
        <Input
          value={draft}
          onChange={(e) => setDraft(e.currentTarget.value)}
          onKeyDown={(e) => {
            // 回车添加而不是提交表单：这个输入框在 <form> 里，默认行为会直接投稿。
            if (e.key === 'Enter') {
              e.preventDefault();
              onAdd();
            }
          }}
          disabled={disabled}
          maxLength={128}
          aria-label="手填模型名"
        />
      </label>
      <span className="pb-1">
        <Button type="button" onClick={onAdd} disabled={disabled || draft.trim() === ''}>
          添加
        </Button>
      </span>
    </div>
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
