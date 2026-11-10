import { useQuery } from '@tanstack/react-query';
import { useState, type FormEvent } from 'react';
import { Link, Navigate } from 'react-router';
import { Button, FileInput, Input, Select, Textarea } from '@/components/ui';
import { ClassificationPicker } from '@/components/classification-picker';
import { ModelPicker } from '@/components/model-picker';
import { listDemoTypes, submitDemo, type DemoType, type Error as ApiError } from '@/api';
import { useAuth } from '@/features/auth/AuthContext';

const MAX_PACKAGE_BYTES = 200 * 1024 * 1024;

/** 目录读不到时的退路：只留最常用的两种，别让投稿页整页不可用。 */
const FALLBACK_TYPES: DemoType[] = [
  { code: 'web', name: '网页作品', formats: ['single:html', 'single:svg', 'zip:html'] },
  { code: 'zip', name: '压缩包作品', formats: ['zip:html'] },
];

// 格式串 → 给作者看的说法 + 浏览器 accept 里的扩展名。
// 这张表只在「怎么显示」这一侧用：能不能投由服务端判，这里只管别让作者白等一次上传。
const FORMAT_INFO: Record<string, { text: string; accept: string[] }> = {
  'single:html': { text: 'HTML', accept: ['.html', '.htm'] },
  'single:svg': { text: 'SVG', accept: ['.svg'] },
  'single:png': { text: 'PNG', accept: ['.png'] },
  'single:jpg': { text: 'JPG', accept: ['.jpg', '.jpeg'] },
  'single:webp': { text: 'WebP', accept: ['.webp'] },
  'single:glb': { text: 'GLB 三维模型', accept: ['.glb'] },
  'zip:html': { text: '含 index.html 的 zip', accept: ['.zip'] },
  'zip:godot': { text: 'Godot 导出的 zip', accept: ['.zip'] },
  url: { text: '外部链接', accept: [] },
};

function formatsText(formats: string[]): string {
  return formats.map((f) => FORMAT_INFO[f]?.text ?? f).join(' / ');
}

/** 把类型的格式清单拼成浏览器 accept；空清单返回空串（不限制）。 */
function acceptFor(formats: string[]): string {
  const exts = formats.flatMap((f) => FORMAT_INFO[f]?.accept ?? []);
  return [...new Set(exts)].join(',');
}

/**
 * 本地挡一道「扩展名不在这个类型里」。
 *
 * 服务端会拦（这是权威判据），但让作者等完一次上传再看到 400 是没必要的。
 * 这里只比扩展名，判据与服务端同一份清单，措辞不同。
 */
function extensionProblem(fileName: string, formats: string[]): string | null {
  const exts = formats.flatMap((f) => FORMAT_INFO[f]?.accept ?? []);
  if (exts.length === 0) return null;
  const dot = fileName.lastIndexOf('.');
  const ext = dot >= 0 ? fileName.slice(dot).toLowerCase() : '';
  if (exts.includes(ext)) return null;
  return `这个类型收 ${formatsText(formats)}，你选的是 ${ext || '没有扩展名的文件'}`;
}

async function fetchDemoTypes(): Promise<DemoType[]> {
  const response = await listDemoTypes();
  if (response.status !== 200) {
    throw new Error(`类型目录加载失败（HTTP ${response.status}）`);
  }
  return response.data.items;
}

export function SubmitPage() {
  const { me, isLoading } = useAuth();

  const [title, setTitle] = useState('');
  const [description, setDescription] = useState('');
  const [demoType, setDemoType] = useState('web');
  // 类型与它收的形态是主数据（`demo_types` + `demo_type_formats`），页面不再写死。
  // 读不到时退回两种最常用的，而不是把投稿页变成错误页。
  const typesQuery = useQuery({
    queryKey: ['demo-types'],
    queryFn: fetchDemoTypes,
    staleTime: 10 * 60 * 1000,
    retry: false,
  });
  const types = typesQuery.data ?? FALLBACK_TYPES;
  const usableTypes = types.filter((t) => t.formats.length > 0);
  const closedTypes = types.filter((t) => t.formats.length === 0);
  const current = usableTypes.find((t) => t.code === demoType) ?? usableTypes[0];
  const currentFormats = current?.formats ?? [];
  // 只收 url 的类型（外链）：不传文件，只填地址。判据用清单而不是类型码——
  // 将来多一个「只收链接」的类型时这里不用改。
  const urlOnly = currentFormats.length === 1 && currentFormats[0] === 'url';
  const [modelSlugs, setModelSlugs] = useState<string[]>([]);
  const [classificationSlugs, setClassificationSlugs] = useState<string[]>([]);
  const [file, setFile] = useState<File | null>(null);
  const [externalUrl, setExternalUrl] = useState('');
  const [fileError, setFileError] = useState<string | undefined>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [done, setDone] = useState<{ slug: string } | null>(null);

  // 会话还在读时不判，避免刷新瞬间把已登录的人弹去登录页。
  if (!isLoading && !me) {
    return <Navigate to="/login?return_to=/submit" replace />;
  }

  const onSubmit = async (event: FormEvent) => {
    event.preventDefault();
    if (urlOnly) {
      const trimmed = externalUrl.trim();
      if (trimmed === '') {
        setFileError('请填写作品地址');
        return;
      }
      if (!/^https?:\/\//i.test(trimmed)) {
        setFileError('作品地址只能以 http:// 或 https:// 开头');
        return;
      }
    } else {
      if (!file) {
        setFileError('请选择要上传的作品文件');
        return;
      }
      const problem = extensionProblem(file.name, currentFormats);
      if (problem) {
        setFileError(problem);
        return;
      }
    }
    setFileError(undefined);
    setBusy(true);
    setError(null);
    try {
      const response = await submitDemo({
        title,
        description: description === '' ? undefined : description,
        demoType,
        // 不传空数组：契约里 modelSlugs 可省略，空数组和不传在服务端是同一个语义
        // （整体替换），但省略能让请求体少一个字段、日志少一行噪音。
        ...(modelSlugs.length > 0 ? { modelSlugs: modelSlugs } : {}),
        // 分类同理：空数组与不传在服务端都是「没打分类」，省略少一个字段。
        ...(classificationSlugs.length > 0 ? { classificationSlugs: classificationSlugs } : {}),
        // 外链作品交地址，普通作品交文件：两者互斥，服务端两个都拒。
        ...(urlOnly ? { externalUrl: externalUrl.trim() } : { package: file as File }),
      });
      // 201 = 新建；200 = 重放（同一个 Idempotency-Key 已经建过）。
      // 浏览器这一侧目前不发这个头，但重放一旦能发生，它就该被当成成功——
      // 否则用户会看到一条空的报错（后端对 200 的 body 是完整回执）。
      if (response.status === 201 || response.status === 200) {
        setDone({ slug: response.data.slug });
        return;
      }
      const body = response.data as ApiError;
      // 413/415/422 是「这个包不行」，429 是「现在不行」。都直接展示后端的中文说明：
      // 这几条消息是写给用户看的，不是内部错误串。
      setError(body.message);
    } catch {
      setError('上传失败，请检查网络后重试');
    } finally {
      setBusy(false);
    }
  };

  if (done) {
    return (
      <section className="mx-auto w-full max-w-2xl px-4 py-12">
        <h1 className="text-2xl font-semibold">投稿已提交</h1>
        <p className="mt-2 text-sm text-gray-600 dark:text-gray-300">
          作品处于待审核状态，通过并发布后才会出现在作品库里。你可以先自己预览一下：
        </p>
        <p className="mt-4 flex gap-3">
          {/*
            这里是预览地址而不是 /demos/{slug}：作品还是 draft + pending，
            而详情页对 draft/pending/rejected 一律返回 404——作者点「查看」会撞死路。
            预览地址对作者本人与审核员开放，正是这个阶段的查看入口。
          */}
          <Link
            to={`/preview/${encodeURIComponent(done.slug)}/`}
            target="_blank"
            rel="noreferrer"
            className="text-blue-600 dark:text-blue-400"
          >
            预览这条作品
          </Link>
          <Link to="/demos" className="text-blue-600 dark:text-blue-400">
            去作品库
          </Link>
        </p>
      </section>
    );
  }

  return (
    <section className="mx-auto w-full max-w-2xl px-4 py-12">
      <h1 className="text-2xl font-semibold">投稿</h1>
      <p className="mt-1 text-sm text-gray-500 dark:text-gray-400">
        提交后进入待审核队列，审核通过并能发布时才会出现在作品库。
      </p>

      <form className="mt-6 flex flex-col gap-4" onSubmit={onSubmit}>
        <label className="flex flex-col gap-1 text-sm">
          标题
          <Input
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            maxLength={200}
            required
          />
        </label>

        <label className="flex flex-col gap-1 text-sm">
          简介（可留空）
          <Textarea
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            maxLength={5000}
            rows={4}
          />
        </label>

        <label className="flex flex-col gap-1 text-sm">
          作品类型
          <Select
            value={current?.code ?? demoType}
            options={usableTypes.map((t) => ({
              value: t.code,
              label: `${t.name}（收 ${formatsText(t.formats)}）`,
            }))}
            onChange={(code) => {
              setDemoType(code);
              setFile(null);
              setExternalUrl('');
              setFileError(undefined);
            }}
            ariaLabel="作品类型"
            disabled={busy}
          />
        </label>

        {current && (
          <p className="m-0 text-xs text-gray-500 dark:text-gray-400">
            这一类收 {formatsText(current.formats)}
            {current.code === 'image' || current.code === 'model'
              ? '；不收压缩包，一张图或一个模型就是一个作品'
              : ''}
          </p>
        )}
        {closedTypes.length > 0 && (
          <p className="m-0 text-xs text-gray-400 dark:text-gray-500">
            暂未开放投稿：
            {closedTypes.map((t) => t.name).join('、')}
          </p>
        )}
        {typesQuery.isError && (
          <p className="m-0 text-xs text-amber-600 dark:text-amber-400">
            类型目录暂时读不到，先按常用的两种显示。
          </p>
        )}

        <ClassificationPicker
          value={classificationSlugs}
          onChange={setClassificationSlugs}
          disabled={busy}
        />

        <ModelPicker value={modelSlugs} onChange={setModelSlugs} disabled={busy} />

        {urlOnly ? (
          <label className="flex flex-col gap-1 text-sm">
            作品地址（http:// 或 https://）
            <Input
              type="url"
              value={externalUrl}
              onChange={(e) => setExternalUrl(e.target.value)}
              placeholder="https://example.com/your-demo"
              disabled={busy}
            />
            {fileError && (
              <span className="text-xs text-red-600 dark:text-red-400" role="alert">
                {fileError}
              </span>
            )}
          </label>
        ) : (
          <FileInput
            label={`选择作品文件（收 ${formatsText(currentFormats)}）`}
            accept={acceptFor(currentFormats)}
            file={file}
            onFileChange={setFile}
            maxBytes={MAX_PACKAGE_BYTES}
            onReject={setFileError}
            error={fileError}
            disabled={busy}
          />
        )}

        {error && (
          <p className="m-0 text-sm text-red-600 dark:text-red-400" role="alert">
            {error}
          </p>
        )}

        <Button type="submit" disabled={busy}>
          {busy ? '提交中…（大文件需要一点时间）' : '提交投稿'}
        </Button>
      </form>
    </section>
  );
}
