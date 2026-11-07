import { useEffect, useState, type FormEvent } from 'react';
import { Link, Navigate, useParams } from 'react-router';
import { Button, ErrorState, FileInput, Input, Select, Skeleton, Textarea } from '@/components/ui';
import { ClassificationPicker } from '@/components/classification-picker';
import { ModelPicker } from '@/components/model-picker';
import { getDemo, updateDemo, type DemoDetail, type Error as ApiError } from '@/api';
import { useAuth } from '@/features/auth/AuthContext';
import { RevisionHistory } from '@/features/demo/revision-history';

const MAX_PACKAGE_BYTES = 200 * 1024 * 1024;

const DEMO_TYPES = [
  { value: 'web', label: '网页作品（单个 HTML/SVG，或一个含 index.html 的 zip）' },
  { value: 'zip', label: '压缩包作品（zip）' },
];

function acceptFor(demoType: string): string {
  return demoType === 'zip'
    ? '.zip,application/zip'
    : '.zip,.html,.htm,.svg,text/html,image/svg+xml';
}

type Loaded = { kind: 'ok'; demo: DemoDetail } | { kind: 'missing' };

async function load(slug: string): Promise<Loaded> {
  const response = await getDemo(slug);
  if (response.status === 200) {
    // 详情接口对「不是作者」的人也会回 200（公开作品），所以这里必须再看 canEdit。
    // 只靠后端 404 是不够的：公开作品的 slug 谁都猜得到。
    return { kind: 'ok', demo: response.data };
  }
  if (response.status === 404) return { kind: 'missing' };
  throw new Error(`读取作品失败：HTTP ${response.status}`);
}

/**
 * 编辑作品。
 *
 * # 为什么标题/简介是预填当前版本，而不是空白
 *
 * `canEdit` 的语义是「你是作者，看得到自己的当前版本」。作者点进编辑页时看到的
 * 应该是他上次写的东西，而不是一张空白表——否则「只改一个错别字」也会把整段简介丢掉。
 *
 * # 为什么文件是可选的
 *
 * 只改文字不产生新的字节，服务端会整表复制清单（零拷贝），版本号 +1。
 * 换文件才需要重新审核。这条规则由后端强制执行，前端这里的说明只是让作者别困惑。
 *
 * # 为什么不给 demoType 加 disabled
 *
 * 换类型必须同时换包（一个 zip 不可能变成单 HTML）。前端把它做成「选了新文件才能改」，
 * 但真正的校验在服务端：不带包改类型直接 400。前端少拦一次，服务端就多暴露一次——
 * 这里选择让服务端当唯一判据。
 */
export function EditDemoPage() {
  const { slug = '' } = useParams();
  const { me, isLoading: authLoading } = useAuth();

  const [state, setState] = useState<Loaded | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);

  const [title, setTitle] = useState('');
  const [description, setDescription] = useState('');
  const [demoType, setDemoType] = useState('web');
  const [modelSlugs, setModelSlugs] = useState<string[]>([]);
  const [classificationSlugs, setClassificationSlugs] = useState<string[]>([]);
  const [file, setFile] = useState<File | null>(null);
  const [fileError, setFileError] = useState<string | undefined>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [done, setDone] = useState<{ revisionNo: number } | null>(null);

  useEffect(() => {
    if (authLoading || !me || slug === '') return;
    let cancelled = false;
    void (async () => {
      try {
        const next = await load(slug);
        if (cancelled) return;
        setState(next);
        if (next.kind === 'ok') {
          setTitle(next.demo.title ?? '');
          setDescription(next.demo.description ?? '');
          setDemoType(next.demo.demoType === '' ? 'web' : next.demo.demoType);
          // 预填当前的声明：编辑表单提交的是「这次声明了哪些」，不预填就等于默认清空。
          setModelSlugs(next.demo.models.map((m) => m.slug));
          // 分类同样要预填。但注意语义差异：服务端把「不传」当作「不改」，
          // 所以这里的预填是「让勾选状态如实反映现状」，而不是「必须交回」。
          setClassificationSlugs(next.demo.classifications.map((c) => c.slug));
        }
      } catch (e) {
        if (!cancelled) setLoadError(e instanceof Error ? e.message : '读取作品失败');
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [authLoading, me, slug]);

  if (!authLoading && !me) {
    return <Navigate to={`/login?return_to=/demos/${encodeURIComponent(slug)}/edit`} replace />;
  }

  if (loadError) {
    return (
      <section className="mx-auto w-full max-w-2xl px-4 py-12">
        <ErrorState
          title="读取作品失败"
          description={loadError}
          onRetry={() => window.location.reload()}
        />
      </section>
    );
  }

  if (!state) {
    return (
      <section className="mx-auto w-full max-w-2xl px-4 py-12">
        <Skeleton lines={4} lineHeight="h-8" />
      </section>
    );
  }

  if (state.kind === 'missing') {
    return (
      <section className="mx-auto w-full max-w-2xl px-4 py-12">
        <h1 className="text-2xl font-semibold">作品不存在</h1>
        <p className="mt-2 text-sm text-gray-600 dark:text-gray-300">
          可能链接有误，或者它已经被删除了。
        </p>
      </section>
    );
  }

  // 能读到作品但不是作者：说明是公开作品的详情，用户没有编辑权。
  if (state.kind === 'ok' && !state.demo.canEdit) {
    return (
      <section className="mx-auto w-full max-w-2xl px-4 py-12">
        <h1 className="text-2xl font-semibold">不能编辑这条作品</h1>
        <p className="mt-2 text-sm text-gray-600 dark:text-gray-300">
          只有作者本人能编辑。如果你确实需要修改，请联系站点管理员。
        </p>
        <p className="mt-4">
          <Link
            to={`/demos/${encodeURIComponent(slug)}`}
            className="text-blue-600 dark:text-blue-400"
          >
            回到作品页
          </Link>
        </p>
      </section>
    );
  }

  const onSubmit = async (event: FormEvent) => {
    event.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const response = await updateDemo(slug, {
        title,
        description: description === '' ? undefined : description,
        // 不给文件就不传 demoType：服务端只允许「换包时顺带换类型」。
        demoType: file ? demoType : undefined,
        // 声明是整体替换语义：这里**总是**把当前勾选交上去，不勾就是清空。
        // 想保留原来的声明，就得让它们继续保持勾选——预填负责这件事。
        modelSlugs: modelSlugs,
        // 分类：总是交回当前勾选。**一个都没勾时不传**——服务端把空数组
        // 与不传都当「不改」，真要清空得另开路径（见松点 48）。
        ...(classificationSlugs.length > 0 ? { classificationSlugs: classificationSlugs } : {}),
        package: file ?? undefined,
      });
      if (response.status === 200) {
        setDone({ revisionNo: response.data.revisionNo });
        return;
      }
      const body = response.data as ApiError;
      setError(body.message);
    } catch {
      setError('保存失败，请检查网络后重试');
    } finally {
      setBusy(false);
    }
  };

  if (done) {
    return (
      <section className="mx-auto w-full max-w-2xl px-4 py-12">
        <h1 className="text-2xl font-semibold">已提交新版</h1>
        <p className="mt-2 text-sm text-gray-600 dark:text-gray-300">
          第 {done.revisionNo} 版已提交，正在等复审。复审期间线上仍然是上一个通过审核的版本，
          读者看到的内容不会因为你在改稿而中断。
        </p>
        <p className="mt-4 flex gap-3">
          <Link
            to={`/preview/${encodeURIComponent(slug)}/`}
            target="_blank"
            rel="noreferrer"
            className="text-blue-600 dark:text-blue-400"
          >
            预览新版
          </Link>
          <Link
            to={`/demos/${encodeURIComponent(slug)}`}
            className="text-blue-600 dark:text-blue-400"
          >
            回到作品页
          </Link>
        </p>
      </section>
    );
  }

  return (
    <section className="mx-auto w-full max-w-2xl px-4 py-12">
      <h1 className="text-2xl font-semibold">编辑作品</h1>
      <p className="mt-1 text-sm text-gray-500 dark:text-gray-400">
        保存后产生新版本并进入复审。旧版本继续对外服务，复审通过才会切换。
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
          作品类型（换类型请同时选择新文件）
          <Select
            value={demoType}
            options={DEMO_TYPES}
            onChange={setDemoType}
            ariaLabel="作品类型"
          />
        </label>

        <ClassificationPicker
          value={classificationSlugs}
          onChange={setClassificationSlugs}
          disabled={busy}
          extra={state.kind === 'ok' ? state.demo.classifications.map((c) => c.slug) : []}
        />

        <ModelPicker
          value={modelSlugs}
          onChange={setModelSlugs}
          disabled={busy}
          extra={state.kind === 'ok' ? state.demo.models.map((m) => m.slug) : []}
        />

        <FileInput
          label="选择新版本文件（不选就只改文字）"
          accept={acceptFor(demoType)}
          file={file}
          onFileChange={setFile}
          maxBytes={MAX_PACKAGE_BYTES}
          onReject={setFileError}
          error={fileError}
          disabled={busy}
        />

        {error && (
          <p className="m-0 text-sm text-red-600 dark:text-red-400" role="alert">
            {error}
          </p>
        )}

        <Button type="submit" disabled={busy}>
          {busy ? '保存中…（大文件需要一点时间）' : '保存并提交复审'}
        </Button>
      </form>

      <RevisionHistory slug={slug} />
    </section>
  );
}
