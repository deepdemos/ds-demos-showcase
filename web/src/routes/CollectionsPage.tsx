import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { Link, Navigate } from 'react-router';
import { Button, EmptyState, ErrorState, Input, Skeleton } from '@/components/ui';
import { Page } from '@/components/layout/page';
import { createCollection, listCollections } from '@/api';
import { useAuth } from '@/features/auth/AuthContext';

/**
 * 我的收藏夹（#438 / COL-003、COL-004、COL-009）。
 *
 * 默认夹由服务端惰性创建；这一页只负责展示与新建自定义夹。
 * 与详情页的「加入指定夹」共用同一个 ['collections'] 查询缓存。
 */
export function CollectionsPage() {
  const { me, isLoading } = useAuth();
  const queryClient = useQueryClient();
  const [name, setName] = useState('');
  const [error, setError] = useState<string | null>(null);

  const list = useQuery({
    queryKey: ['collections'],
    queryFn: async () => {
      const response = await listCollections();
      if (response.status !== 200) throw new Error('收藏夹加载失败（' + response.status + '）');
      return response.data;
    },
    enabled: me !== null,
  });

  const create = useMutation({
    mutationFn: async (collectionName: string) => {
      const response = await createCollection({ name: collectionName });
      if (response.status !== 201) {
        const message =
          typeof response.data === 'object' && response.data && 'message' in response.data
            ? String((response.data as { message?: unknown }).message)
            : '新建收藏夹失败（' + response.status + '）';
        throw new Error(message);
      }
      return response.data;
    },
    onSuccess: () => {
      setName('');
      setError(null);
      void queryClient.invalidateQueries({ queryKey: ['collections'] });
    },
    onError: (err) => setError(err instanceof Error ? err.message : '新建收藏夹失败'),
  });

  if (isLoading) return <Skeleton lines={4} />;
  if (me === null) return <Navigate to="/login?return_to=/me/collections" replace />;

  const items = list.data?.items ?? [];
  const defaultCollection = items.find((item) => item.isDefault);
  const custom = items.filter((item) => !item.isDefault);

  return (
    <Page
      title="我的收藏夹"
      description="默认收藏夹始终私有；自定义夹可以选择公开，公开后访客可读（但不能写）。"
    >
      <form
        className="mt-4 flex flex-wrap items-end gap-2"
        onSubmit={(event) => {
          event.preventDefault();
          const trimmed = name.trim();
          if (!trimmed) {
            setError('名称不能为空');
            return;
          }
          create.mutate(trimmed);
        }}
      >
        <label className="min-w-48 flex-1 text-sm">
          <span className="text-gray-500">新建收藏夹</span>
          <Input
            fullWidth
            className="mt-1"
            value={name}
            maxLength={50}
            placeholder="例如：灵感收藏"
            onChange={(event) => setName(event.target.value)}
          />
        </label>
        <Button type="submit" primary disabled={create.isPending || name.trim() === ''}>
          新建
        </Button>
      </form>

      {error ? (
        <p role="alert" className="mt-2 text-sm text-red-600">
          {error}
        </p>
      ) : null}

      {list.isError ? (
        <div className="mt-4">
          <ErrorState
            title="收藏夹加载失败"
            description="请稍后重试。"
            onRetry={() => void list.refetch()}
            retrying={list.isFetching}
          />
        </div>
      ) : null}

      {list.isPending ? (
        <div className="mt-4">
          <Skeleton lines={3} />
        </div>
      ) : null}

      {list.data && items.length === 0 ? (
        <div className="mt-4">
          <EmptyState title="还没有收藏夹" description="默认收藏夹会在第一次收藏时自动创建。" />
        </div>
      ) : null}

      {items.length > 0 ? (
        <ul className="mt-6 list-none space-y-3 p-0">
          {[defaultCollection, ...custom].filter(Boolean).map((collection) =>
            collection ? (
              <li key={collection.id}>
                <Link
                  to={'/me/collections/' + collection.id}
                  className="flex items-center justify-between gap-3 rounded-lg border border-gray-200 p-4 text-inherit no-underline hover:border-gray-300"
                >
                  <div className="min-w-0">
                    <p className="truncate font-medium text-gray-900">
                      {collection.name}
                      {collection.isDefault ? (
                        <span className="ml-2 text-xs text-gray-500">默认</span>
                      ) : null}
                    </p>
                    {collection.description ? (
                      <p className="mt-1 truncate text-sm text-gray-500">
                        {collection.description}
                      </p>
                    ) : null}
                  </div>
                  <div className="flex flex-none items-center gap-3 text-xs text-gray-500">
                    <span>{collection.visibility === 'public' ? '公开' : '私密'}</span>
                    <span>{collection.itemCount} 件</span>
                  </div>
                </Link>
              </li>
            ) : null,
          )}
        </ul>
      ) : null}
    </Page>
  );
}
