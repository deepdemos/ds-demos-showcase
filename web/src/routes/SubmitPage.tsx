import { useState, type FormEvent } from 'react';
import { Link, Navigate } from 'react-router';
import { Button, FileInput, Input, Select, Textarea } from '@/components/ui';
import { ModelPicker } from '@/components/model-picker';
import { submitDemo, type Error as ApiError } from '@/api';
import { useAuth } from '@/features/auth/AuthContext';

const MAX_PACKAGE_BYTES = 200 * 1024 * 1024;

const DEMO_TYPES = [
  { value: 'web', label: '网页作品（单个 HTML/SVG，或一个含 index.html 的 zip）' },
  { value: 'zip', label: '压缩包作品（zip）' },
];

/** 按类型给 accept：选错文件时浏览器会先挡一道。 */
function acceptFor(demoType: string): string {
  return demoType === 'zip'
    ? '.zip,application/zip'
    : '.zip,.html,.htm,.svg,text/html,image/svg+xml';
}

export function SubmitPage() {
  const { me, isLoading } = useAuth();

  const [title, setTitle] = useState('');
  const [description, setDescription] = useState('');
  const [demoType, setDemoType] = useState('web');
  const [modelSlugs, setModelSlugs] = useState<string[]>([]);
  const [file, setFile] = useState<File | null>(null);
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
    if (!file) {
      setFileError('请选择要上传的作品文件');
      return;
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
        package: file,
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
            value={demoType}
            options={DEMO_TYPES}
            onChange={setDemoType}
            ariaLabel="作品类型"
          />
        </label>

        <ModelPicker value={modelSlugs} onChange={setModelSlugs} disabled={busy} />

        <FileInput
          label="选择作品文件（zip，或单个 html/svg）"
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
          {busy ? '提交中…（大文件需要一点时间）' : '提交投稿'}
        </Button>
      </form>
    </section>
  );
}
