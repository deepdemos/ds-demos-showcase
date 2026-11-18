import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useEffect, useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router';
import { Button, EmptyState, ErrorState, Input, Select, Skeleton, Textarea } from '@/components/ui';
import {
  deleteCollection,
  getCollection,
  listCollections,
  removeCollectionItem,
  updateCollection,
} from '@/api';
import { useAuth } from '@/features/auth/AuthContext';

/**
 * 收藏夹详情（#438 / COL-005、COL-009、COL-010、COL-014）。
 *
 * 公开夹访客可读；私有夹非主人由服务端回 404，这一页落「不存在或不可见」。
 * 管理控件只在「主人」出现：用 /collections 列表判断，而不是让前端猜 id。
 */

const VISIBILITY_OPTIONS = [
  { value: 'private', label: '私密' },
  { value: 'public', label: '公开' },
];

export function CollectionDetailPage() {
  const params = useParams();
  const id = Number(params.id);
  const { me } = useAuth();
  const queryClient = useQueryClient();
  const navigate = useNavigate();

  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [visibility, setVisibility] = useState<'private' | 'public'>('private');
  const [confirmingDelete, setConfirmingDelete] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const detail = useQuery({
    queryKey: ['collection', id],
    queryFn: async () => {
      const response = await getCollection(id);
      if (response.status === 404) return null;
      if (response.status !== 200) throw new Error('收藏夹加载失败（' + response.status + '）');
      return response.data;
    },
    enabled: Number.isInteger(id) && id > 0,
  });

  const mine = useQuery({
    queryKey: ['collections'],
    queryFn: async () => {
      const response = await listCollections();
      if (response.status !== 200) throw new Error('收藏夹加载失败（' + response.status + '）');
      return response.data;
    },
    enabled: me !== null,
  });

  const collection = detail.data?.collection;

  useEffect(() => {
    if (!collection) return;
    setName(collection.name);
    setDescription(collection.description);
    setVisibility(collection.visibility);
  }, [collection]);

  const invalidate = () => {
    void queryClient.invalidateQueries({ queryKey: ['collection', id] });
    void queryClient.invalidateQueries({ queryKey: ['collections'] });
    void queryClient.invalidateQueries({ queryKey: ['collection-status'] });
  };

  const save = useMutation({
    mutationFn: async () => {
      const response = await updateCollection(id, { name, description, visibility });
      if (response.status !== 200) {
        const message =
          typeof response.data === 'object' && response.data && 'message' in response.data
            ? String((response.data as { message?: unknown }).message)
            : '保存失败（' + response.status + '）';
        throw new Error(message);
      }
      return response.data;
    },
    onSuccess: () => {
      setError(null);
      invalidate();
    },
    onError: (err) => setError(err instanceof Error ? err.message : '保存失败'),
  });

  const remove = useMutation({
    mutationFn: async () => {
      const response = await deleteCollection(id);
      if (response.status !== 204) throw new Error('删除失败（' + response.status + '）');
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['collections'] });
      navigate('/me/collections');
    },
    onError: (err) => setError(err instanceof Error ? err.message : '删除失败'),
  });

  const removeItem = useMutation({
    mutationFn: async (slug: string) => {
      const response = await removeCollectionItem(id, slug);
      if (response.status !== 204) throw new Error('移出失败（' + response.status + '）');
    },
    onSuccess: () => {
      setError(null);
      invalidate();
    },
    onError: (err) => setError(err instanceof Error ? err.message : '移出失败'),
  });

  if (!Number.isInteger(id) || id <= 0) {
    return (
      <section className="mx-auto w-full max-w-3xl px-4 py-8">
        <EmptyState title="收藏夹不存在" description="地址里的编号不合法。" />
      </section>
    );
  }

  if (detail.isPending) {
    return (
      <section className="mx-auto w-full max-w-3xl px-4 py-8">
        <Skeleton lines={4} />
      </section>
    );
  }

  if (detail.isError) {
    return (
      <section className="mx-auto w-full max-w-3xl px-4 py-8">
        <ErrorState
          title="收藏夹加载失败"
          description="请稍后重试。"
          onRetry={() => void detail.refetch()}
          retrying={detail.isFetching}
        />
      </section>
    );
  }

  if (detail.data === null) {
    return (
      <section className="mx-auto w-full max-w-3xl px-4 py-8">
        <EmptyState
          title="收藏夹不存在或不可见"
          description="它可能是私密收藏夹，或者已经被删除。"
          action={
            <Link className="text-sm underline" to="/me/collections">
              回我的收藏夹
            </Link>
          }
        />
      </section>
    );
  }

  const data = detail.data;
  const isOwner =
    me !== null && (mine.data?.items.some((item) => item.id === data.collection.id) ?? false);
  const manage = isOwner && !data.collection.isDefault;

  return (
    <section className="mx-auto w-full max-w-3xl px-4 py-8">
      <header className="space-y-1">
        <p className="text-sm text-gray-500 dark:text-gray-400">
          <Link className="underline" to="/me/collections">
            我的收藏夹
          </Link>
          {data.collection.visibility === 'public' ? ' · 公开' : ' · 私密'}
        </p>
        <h1 className="text-2xl font-semibold text-gray-900 dark:text-gray-100">
          {data.collection.name}
          {data.collection.isDefault ? (
            <span className="ml-2 text-sm font-normal text-gray-500">默认</span>
          ) : null}
        </h1>
        {data.collection.description ? (
          <p className="whitespace-pre-line text-sm text-gray-700 dark:text-gray-300">
            {data.collection.description}
          </p>
        ) : null}
        <p className="text-sm text-gray-500 dark:text-gray-400">
          共 {data.collection.itemCount} 件
        </p>
      </header>

      {data.collection.isDefault ? (
        <p className="mt-4 rounded-lg border border-gray-200 p-3 text-sm text-gray-600 dark:border-gray-800 dark:text-gray-300">
          默认收藏夹永远私有，不能改名、删除或转公开；作品可以从这里移出。
        </p>
      ) : null}

      {manage ? (
        <section className="mt-4 space-y-3 rounded-lg border border-gray-200 p-4 dark:border-gray-800">
          <h2 className="text-sm font-medium text-gray-700 dark:text-gray-200">管理</h2>
          <label className="block text-sm">
            <span className="text-gray-500 dark:text-gray-400">名称</span>
            <Input
              className="mt-1"
              value={name}
              maxLength={50}
              onChange={(event) => setName(event.target.value)}
            />
          </label>
          <label className="block text-sm">
            <span className="text-gray-500 dark:text-gray-400">说明</span>
            <Textarea
              className="mt-1"
              rows={2}
              maxLength={500}
              value={description}
              onChange={(event) => setDescription(event.target.value)}
            />
          </label>
          <label className="block text-sm">
            <span className="text-gray-500 dark:text-gray-400">可见性</span>
            <Select
              ariaLabel="收藏夹可见性"
              value={visibility}
              options={VISIBILITY_OPTIONS}
              onChange={(value) => setVisibility(value === 'public' ? 'public' : 'private')}
            />
          </label>

          <div className="flex flex-wrap items-center gap-2">
            <Button type="button" primary disabled={save.isPending} onClick={() => save.mutate()}>
              保存
            </Button>
            {confirmingDelete ? (
              <>
                <span className="text-sm text-red-600 dark:text-red-400">
                  删除收藏夹不会删除作品，确认？
                </span>
                <Button
                  type="button"
                  danger
                  disabled={remove.isPending}
                  onClick={() => remove.mutate()}
                >
                  确认删除
                </Button>
                <Button type="button" onClick={() => setConfirmingDelete(false)}>
                  取消
                </Button>
              </>
            ) : (
              <Button type="button" onClick={() => setConfirmingDelete(true)}>
                删除收藏夹
              </Button>
            )}
          </div>
        </section>
      ) : null}

      {error ? (
        <p role="alert" className="mt-3 text-sm text-red-600 dark:text-red-400">
          {error}
        </p>
      ) : null}

      {data.items.length === 0 ? (
        <div className="mt-6">
          <EmptyState
            title="这个收藏夹还是空的"
            description="在作品详情页点「收藏」就会出现在这里。"
          />
        </div>
      ) : (
        <ul className="mt-6 list-none space-y-3 p-0">
          {data.items.map((item) => (
            <li
              key={item.slug}
              data-slug={item.slug}
              data-accessible={item.accessible}
              className="flex items-center gap-3 rounded-lg border border-gray-200 p-3 dark:border-gray-800"
            >
              {item.coverUrl ? (
                <img
                  src={item.coverUrl}
                  alt=""
                  className="h-16 w-24 flex-none rounded object-cover"
                  loading="lazy"
                />
              ) : (
                <div
                  aria-hidden="true"
                  className="h-16 w-24 flex-none rounded bg-gray-100 dark:bg-gray-800"
                />
              )}
              <div className="min-w-0 flex-1">
                {item.accessible ? (
                  <Link
                    to={'/demos/' + item.slug}
                    className="block truncate text-base font-medium text-gray-900 hover:underline dark:text-gray-100"
                  >
                    {item.title || item.slug}
                  </Link>
                ) : (
                  <>
                    <p className="truncate text-base font-medium text-gray-500 dark:text-gray-400">
                      {item.title || item.slug}
                    </p>
                    <p className="mt-1 text-xs text-gray-500 dark:text-gray-400">
                      该作品已不可访问
                    </p>
                  </>
                )}
              </div>
              {isOwner ? (
                <Button
                  type="button"
                  aria-label={'移出 ' + (item.title || item.slug)}
                  disabled={removeItem.isPending}
                  onClick={() => removeItem.mutate(item.slug)}
                >
                  移出
                </Button>
              ) : null}
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
