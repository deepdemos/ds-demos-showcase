import { useQuery } from '@tanstack/react-query';
import { listClassifications, type Classification, type ClassificationCatalog } from '@/api';
import { Button, Checkbox } from '@/components/ui';

// 分类是「这件作品属于什么」，与模型声明不同：分类挂作品、模型挂版本。
//
// 目录来自 `GET /api/v1/classifications`，不写死在前端：三份维度清单（库、契约、前端）
// 迟早不一致，而「维度叫什么、按什么顺序排」正好是该由后端说的事。
//
// # 为什么不给 `mode=open` 的维度做「自己填一个」
//
// 契约里 `subject` 的 mode 是 `open`（允许作者提交新词条待审），但**那条路径还没实现**：
// 服务端 `resolveClassificationSlugs` 只认库里已有的 slug，填一个新词条会 400。
// 与其做一个必然失败的输入框，不如直接说明「现在还不能自助新增」——让作者当场知道，
// 而不是打完字、传完包才被告知。
async function fetchCatalog(): Promise<ClassificationCatalog> {
  const response = await listClassifications();
  if (response.status === 200) return response.data;
  throw new Error(`分类目录加载失败（HTTP ${response.status}）`);
}

/** 按维度分组，组内按展示名排。 */
function groupByDimension(items: Classification[]): Map<string, Classification[]> {
  const groups = new Map<string, Classification[]>();
  for (const item of items) {
    const list = groups.get(item.dimension) ?? [];
    list.push(item);
    groups.set(item.dimension, list);
  }
  for (const list of groups.values()) {
    list.sort((a, b) => a.label.localeCompare(b.label, 'zh-CN'));
  }
  return groups;
}

export type ClassificationPickerProps = {
  /** 已选中的分类 slug（跨维度混在一起，维度从目录反查）。 */
  value: string[];
  onChange: (next: string[]) => void;
  disabled?: boolean;
  /**
   * 作品上已有、但当前目录里查不到的 slug（词条被下架或改名）。
   *
   * 不给它的话，预填时这些 slug 既不在勾选列表里、也不在 value 里，
   * 保存一次就被悄悄清掉——作者看不出发生过什么。
   */
  extra?: string[];
};

export function ClassificationPicker({
  value,
  onChange,
  disabled,
  extra = [],
}: ClassificationPickerProps) {
  const { data, isPending, isError, refetch, isFetching } = useQuery({
    queryKey: ['classifications'],
    queryFn: fetchCatalog,
    staleTime: 5 * 60 * 1000,
  });

  const toggle = (slug: string, checked: boolean) => {
    onChange(checked ? [...value, slug] : value.filter((s) => s !== slug));
  };

  if (isPending) {
    return <p className="m-0 text-sm text-gray-500 dark:text-gray-400">分类目录加载中…</p>;
  }
  // 目录读不到时**不挡投稿**：分类是补充信息，读不到就当作这次不分类。
  // 挡住的代价是作者上传一个大包之后才发现提交不了。
  if (isError || !data) {
    return (
      <div className="flex flex-col gap-1 text-sm">
        <span className="text-gray-600 dark:text-gray-300">
          分类目录加载失败，可以这次先不打分类，稍后在编辑页补。
        </span>
        <span>
          <Button type="button" inline onClick={() => void refetch()} disabled={isFetching}>
            {isFetching ? '重试中…' : '重试'}
          </Button>
        </span>
      </div>
    );
  }

  const groups = groupByDimension(data.items);
  const known = new Set(data.items.map((c) => c.slug));
  // 目录里没有、但作品上带着的：仍要显示出来，否则作者既看不到也删不掉。
  const orphans = [...new Set([...extra, ...value])].filter((s) => !known.has(s));

  return (
    <fieldset className="m-0 flex flex-col gap-3 border-0 p-0">
      <legend className="p-0 text-sm">
        分类
        <span className="ml-2 text-xs text-gray-500 dark:text-gray-400">
          （可选，可多选；分类挂在作品上，改稿不影响）
        </span>
      </legend>

      {data.dimensions.map((dim) => {
        const list = groups.get(dim.dimension) ?? [];
        const picked = value.filter((s) => list.some((c) => c.slug === s));
        return (
          <div key={dim.dimension} className="flex flex-col gap-1">
            <p className="m-0 text-xs text-gray-600 dark:text-gray-400">
              {dim.label}
              {dim.description ? ` — ${dim.description}` : ''}
              {picked.length > 0 ? `（已选 ${picked.length}）` : ''}
            </p>
            {list.length === 0 ? (
              <p className="m-0 text-xs text-gray-500 dark:text-gray-500">这个维度还没有词条。</p>
            ) : (
              <div className="flex flex-wrap gap-x-4 gap-y-1">
                {list.map((item) => (
                  <Checkbox
                    key={item.slug}
                    label={item.label}
                    checked={value.includes(item.slug)}
                    indeterminate={false}
                    disabled={disabled}
                    onChange={(e) => toggle(item.slug, e.target.checked)}
                  />
                ))}
              </div>
            )}
            {dim.mode === 'open' ? (
              <p className="m-0 text-xs text-gray-500 dark:text-gray-500">
                这个维度是开放的，但自助新增还没做——缺了哪一条可以告诉管理员。
              </p>
            ) : null}
          </div>
        );
      })}

      {orphans.length > 0 ? (
        <div className="flex flex-col gap-1">
          <p className="m-0 text-xs text-gray-600 dark:text-gray-400">
            作品上还有这些分类，但当前目录里查不到（可能已下架或改名）：
          </p>
          <div className="flex flex-wrap gap-x-4 gap-y-1">
            {orphans.map((slug) => (
              <Checkbox
                key={slug}
                label={`${slug}（已不在目录中）`}
                checked={value.includes(slug)}
                indeterminate={false}
                disabled={disabled}
                onChange={(e) => toggle(slug, e.target.checked)}
              />
            ))}
          </div>
        </div>
      ) : null}
    </fieldset>
  );
}
