import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useEffect, useState } from 'react';
import { Link } from 'react-router';

import { Button, Input, Select } from '@/components/ui';
import { createModel, listModelVendors, suggestModelSlug, type Model } from '@/api';

/**
 * 管理端「手动新建模型」（模型域 §4.7.3）。
 *
 * # 表单只有三件必填 / 半填的东西
 *
 * 名字必填；slug 预填**服务端给的建议值**且可改；厂商可以选已有的一家、就地新建一家、
 * 或者留空（= 厂商未标注）。没有「状态」「名称精确度」这两个输入：管理员点下去就是
 * 背书（直接 active），人工建的就是确定的型号（直接 exact）。把它们做成下拉，等于让
 * 操作者自己决定「我这句话算不算数」——而这两个状态在读取侧完全一样，选错了没人会发现。
 *
 * # 为什么厂商是「下拉 + 就地新建」而不是一个自由文本输入框
 *
 * 自由文本让同一家厂商出现 DeepSeek / deepseek / 深度求索 三种写法，而模型页要显示的
 * 是**厂商实体**的名字。下拉里没有的那家可以就地建（只收名称），建出来的 slug 由服务端
 * 折——两家折出同一个 slug 会 409 要求改名，不自动加后缀。
 *
 * # 为什么 slug 建议值要问服务端
 *
 * slug 是公开 URL 的一部分（/models/<slug>）且创建后不可变。规则只该有一份实现：
 * 前端再写一遍，两处迟早分叉，而分叉的表现是「表单里看到的地址和实际写上地址不同」。
 * 厂商名也一起带过去：不填 slug 时服务端按「名字 + 厂商名」算，两边输入一致才对得上。
 *
 * # 为什么撞名要给链接而不是自己加后缀
 *
 * 自动后缀（deepseek-chat-2）会造出一个永久难看的展示名，而管理员真正想的多半是
 * 「补全那一条」。所以 409 之后给两条出路：去已有那条看（/models/<slug>）、
 * 或者用上面的导入区块把上游档案补进去。
 *
 * # 界面求最简
 *
 * 不要动效、不要图标、不写营销文案；只给功能性提示。
 */

/** 建议值的防抖：边打边算会把每个按键都变成一次请求。 */
const SUGGEST_DEBOUNCE_MS = 300;

/**
 * 厂商下拉里「新建一家」那个选项的值。
 *
 * 用 `__new__` 而不是空串：空串是「不标注厂商」，两者是不同动作。也不与数字 id 撞
 * （选项值来自 `vendors.id`，都是数字）。
 */
const NEW_VENDOR = '__new__';

interface Problem {
  message: string;
  /** 撞名时已有那条**模型**的 slug；界面据此给链接。 */
  existingSlug?: string;
}

export function ModelCreate() {
  const queryClient = useQueryClient();
  const [name, setName] = useState('');
  // 厂商选择：'' = 不标注，NEW_VENDOR = 就地新建（名字在下一个输入框里），其余是 vendors.id。
  const [vendorChoice, setVendorChoice] = useState('');
  const [newVendorName, setNewVendorName] = useState('');
  const [slug, setSlug] = useState('');
  // 手改过 slug 就不再让建议值覆盖它；清空 slug 视为「请帮我生成」。
  const [slugEdited, setSlugEdited] = useState(false);
  const [problem, setProblem] = useState<Problem | null>(null);
  const [created, setCreated] = useState<Model | null>(null);
  const [debouncedName, setDebouncedName] = useState('');
  const [debouncedVendor, setDebouncedVendor] = useState('');

  // 厂商下拉的数据源。读不到不阻断新建：厂商可留空，也可以直接就地新建——
  // 下拉只是「选已有那家」的快捷方式。
  const vendors = useQuery({
    queryKey: ['admin', 'models', 'vendors'],
    queryFn: async () => {
      const res = await listModelVendors();
      // orval 生成的函数对非 2xx 不 throw：不自己判，403 会变成「空下拉」——
      // 而空下拉看起来就像「一家厂商都还没建过」，两件事的下一步完全不同。
      if (res.status !== 200) throw new Error('厂商列表读取失败（HTTP ' + res.status + '）');
      return res.data.items;
    },
    retry: false,
    staleTime: 5 * 60 * 1000,
  });

  const vendorOptions = [
    { value: '', label: '不标注厂商' },
    ...(vendors.data ?? []).map((vendor) => ({ value: String(vendor.id), label: vendor.name })),
    { value: NEW_VENDOR, label: '新建一家厂商…' },
  ];
  const selectedVendor = vendors.data?.find((vendor) => String(vendor.id) === vendorChoice) ?? null;
  // 厂商名同时喂给两处：slug 建议值（服务端要用它算）与提交体（就地新建时这就是名字）。
  const vendorName =
    vendorChoice === NEW_VENDOR ? newVendorName.trim() : (selectedVendor?.name ?? '');

  // 防抖：300ms 内连续输入只发一次请求。名字与厂商一起进键，两者都影响 slug。
  useEffect(() => {
    const timer = window.setTimeout(() => {
      setDebouncedName(name.trim());
      setDebouncedVendor(vendorName);
    }, SUGGEST_DEBOUNCE_MS);
    return () => window.clearTimeout(timer);
  }, [name, vendorName]);

  const suggestion = useQuery({
    queryKey: ['admin', 'models', 'slug-suggestion', debouncedName, debouncedVendor],
    queryFn: async () => {
      const res = await suggestModelSlug({
        name: debouncedName,
        vendor: debouncedVendor === '' ? undefined : debouncedVendor,
      });
      // orval 生成的函数对非 2xx 不 throw：不自己判，403 会变成「一直没有建议值」。
      if (res.status !== 200) throw new Error('slug 建议值读取失败（HTTP ' + res.status + '）');
      return res.data.slug;
    },
    enabled: debouncedName !== '' && !slugEdited,
    retry: false,
    staleTime: 5 * 60 * 1000,
  });

  useEffect(() => {
    if (slugEdited) return;
    if (suggestion.data) setSlug(suggestion.data);
  }, [suggestion.data, slugEdited]);

  const create = useMutation({
    mutationFn: async () =>
      createModel({
        name: name.trim(),
        ...(slug.trim() === '' ? {} : { slug: slug.trim() }),
        // 选已有与就地新建是互斥的两条路（服务端也这么判）：同时给会让「哪一家」
        // 变成猜的，而猜错会写进共享目录。
        ...(selectedVendor ? { vendorId: selectedVendor.id } : {}),
        ...(!selectedVendor && vendorName !== '' ? { vendor: vendorName } : {}),
      }),
    onSuccess: (res) => {
      // 先取一次状态码：下面按 res.status 逐个收窄之后，res 在兜底分支里已经是 never，
      // 但那恰恰是要留一个「契约以后多了个状态码」的出口。
      const status: number = res.status;
      if (res.status === 201) {
        setProblem(null);
        setCreated(res.data);
        setName('');
        setVendorChoice('');
        setNewVendorName('');
        setSlug('');
        setSlugEdited(false);
        void queryClient.invalidateQueries({ queryKey: ['models'] });
        void queryClient.invalidateQueries({ queryKey: ['admin', 'models'] });
        return;
      }
      setCreated(null);
      if (res.status === 409) {
        // `existingSlug` 只在撞的是**模型**时出现（契约里写死了这条）。厂商名撞 slug
        // 的 409 不带它，所以这里自然只会显示文案、不会给出错的模型链接。
        setProblem({ message: res.data.message, existingSlug: res.data.existingSlug });
        return;
      }
      if (res.status === 400) {
        // 400 的文案是写给操作者看的（「兜底名不能手动新建」「两个厂商字段只能给一个」），直接展示。
        setProblem({ message: res.data.message });
        return;
      }
      if (res.status === 403) {
        setProblem({ message: '没有新建模型的权限（需要 model.create）' });
        return;
      }
      if (res.status === 401) {
        setProblem({ message: '请先登录' });
        return;
      }
      if (res.status === 503) {
        setProblem({ message: '服务暂时不可用，稍后再试' });
        return;
      }
      setProblem({ message: '新建失败（HTTP ' + status + '）' });
    },
    onError: () => {
      setCreated(null);
      setProblem({ message: '请求没发出去，检查网络后重试' });
    },
  });

  const pending = create.isPending;

  return (
    <section className="rounded-xl border border-gray-200 p-4">
      <h2 className="text-sm font-medium text-gray-900">手动新建模型</h2>
      <p className="mt-1 text-xs text-gray-600">
        建出来就是可用状态（active），名称精确度按确定型号（exact）记。同一个名字只能有一条；
        上游目录里已有的模型请走导入，那条会连档案一起补齐。
      </p>

      <form
        className="mt-3 flex flex-col gap-3"
        onSubmit={(event) => {
          event.preventDefault();
          setProblem(null);
          create.mutate();
        }}
      >
        <label className="flex flex-col gap-1 text-sm">
          模型名
          <Input
            fullWidth
            value={name}
            maxLength={128}
            placeholder="例如 DeepSeek V4.1 Flash"
            onChange={(event) => setName(event.target.value)}
          />
        </label>
        <label className="flex flex-col gap-1 text-sm">
          厂商（可留空）
          <Select
            value={vendorChoice}
            options={vendorOptions}
            onChange={(value) => {
              setVendorChoice(value);
              if (value !== NEW_VENDOR) setNewVendorName('');
            }}
          />
        </label>
        {vendorChoice === NEW_VENDOR ? (
          <>
            <label className="flex flex-col gap-1 text-sm">
              新厂商名
              <Input
                fullWidth
                value={newVendorName}
                maxLength={64}
                placeholder="例如 智谱"
                onChange={(event) => setNewVendorName(event.target.value)}
              />
            </label>
            <p className="m-0 text-xs text-gray-600">
              只填名称：建出来之后它会出现在下拉里，以后直接选。名字折出的 slug
              已被另一家占用时会被要求改名。
            </p>
          </>
        ) : null}
        <label className="flex flex-col gap-1 text-sm">
          slug（进 /models/&lt;slug&gt;，创建后不可改）
          <Input
            fullWidth
            value={slug}
            maxLength={128}
            placeholder="按名字自动生成"
            onChange={(event) => {
              setSlug(event.target.value);
              setSlugEdited(event.target.value.trim() !== '');
            }}
          />
        </label>

        <div className="flex flex-wrap items-center gap-3">
          <Button type="submit" primary disabled={pending}>
            {pending ? '新建中…' : '新建模型'}
          </Button>
          {slugEdited ? (
            <span className="text-xs text-gray-600">slug 是你手填的，不会跟着名字变</span>
          ) : null}
          {suggestion.isError ? (
            <span className="text-xs text-gray-600">建议值暂时取不到，可以自己填 slug</span>
          ) : null}
          {vendors.isError ? (
            <span className="text-xs text-gray-600">
              厂商下拉暂时读不到（已有厂商无法选）；可以先不标注，或用上面的「新建一家厂商」填名字
            </span>
          ) : null}
        </div>
      </form>

      {problem ? (
        <div role="alert" className="mt-3 text-sm text-red-700">
          <p>{problem.message}</p>
          {problem.existingSlug ? (
            <p className="mt-1">
              已有那条：
              <Link className="underline" to={'/models/' + problem.existingSlug}>
                {problem.existingSlug}
              </Link>
              。要把它补全，用上面的导入区块按 slug 搜一遍即可。
            </p>
          ) : null}
        </div>
      ) : null}

      {created ? (
        <p role="status" className="mt-3 text-sm text-green-700">
          已新建{' '}
          <Link className="underline" to={'/models/' + created.slug}>
            {created.name}
          </Link>
          （{created.slug}），它已经出现在模型目录里。
        </p>
      ) : null}
    </section>
  );
}
