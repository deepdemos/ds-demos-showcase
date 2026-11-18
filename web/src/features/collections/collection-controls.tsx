import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { Link, useLocation } from 'react-router';
import { Button, Checkbox, Skeleton } from '@/components/ui';
import {
  addCollectionItem,
  getCollectionStatus,
  listCollections,
  removeCollectionItem,
} from '@/api';
import { useAuth } from '@/features/auth/AuthContext';

/**
 * 详情页的收藏入口（#438 / COL-001、COL-008）。
 *
 * 两条路径不混用：
 *   - 「收藏」按钮 = 切换默认夹；
 *   - 「加入指定夹」= 展开自定义夹逐个勾选。
 *
 * 未登录时只给一句登录提示：写请求在服务端是 401/403，前端不给一个注定失败的按钮。
 */
export function CollectionControls({ slug }: { slug: string }) {
  const { me, isLoading } = useAuth();
  const location = useLocation();
  const queryClient = useQueryClient();
  const [error, setError] = useState<string | null>(null);

  const status = useQuery({
    queryKey: ['collection-status', slug],
    queryFn: async () => {
      const response = await getCollectionStatus({ slug });
      if (response.status !== 200) throw new Error('收藏状态加载失败（' + response.status + '）');
      return response.data;
    },
    enabled: me !== null,
  });

  const collections = useQuery({
    queryKey: ['collections'],
    queryFn: async () => {
      const response = await listCollections();
      if (response.status !== 200) throw new Error('收藏夹加载失败（' + response.status + '）');
      return response.data;
    },
    enabled: me !== null,
  });

  const invalidate = () => {
    void queryClient.invalidateQueries({ queryKey: ['collection-status', slug] });
    void queryClient.invalidateQueries({ queryKey: ['collections'] });
    void queryClient.invalidateQueries({ queryKey: ['collection'] });
  };

  const add = useMutation({
    mutationFn: async (collectionID: number) => {
      const response = await addCollectionItem(collectionID, { slug });
      if (response.status !== 204) throw new Error('收藏失败（' + response.status + '）');
    },
    onSuccess: () => {
      setError(null);
      invalidate();
    },
    onError: (err) => setError(err instanceof Error ? err.message : '收藏失败'),
  });

  const remove = useMutation({
    mutationFn: async (collectionID: number) => {
      const response = await removeCollectionItem(collectionID, slug);
      if (response.status !== 204) throw new Error('取消收藏失败（' + response.status + '）');
    },
    onSuccess: () => {
      setError(null);
      invalidate();
    },
    onError: (err) => setError(err instanceof Error ? err.message : '取消收藏失败'),
  });

  if (isLoading) return null;

  if (me === null) {
    return (
      <p className="mt-4 text-sm text-gray-500 dark:text-gray-400">
        登录后可收藏这件作品。
        <Link
          className="ml-2 underline"
          to={'/login?return_to=' + encodeURIComponent(location.pathname)}
        >
          去登录
        </Link>
      </p>
    );
  }

  if (status.isPending || collections.isPending) {
    return (
      <div className="mt-4">
        <Skeleton lines={1} />
      </div>
    );
  }

  const items = collections.data?.items ?? [];
  const defaultCollection = items.find((item) => item.isDefault);
  const inDefault = status.data?.inDefault ?? false;
  const custom = items.filter((item) => !item.isDefault);
  const busy = add.isPending || remove.isPending;

  return (
    <section className="mt-4 space-y-2" aria-label="收藏">
      <div className="flex flex-wrap items-center gap-2">
        <Button
          type="button"
          disabled={!defaultCollection || busy}
          onClick={() => {
            if (!defaultCollection) return;
            if (inDefault) remove.mutate(defaultCollection.id);
            else add.mutate(defaultCollection.id);
          }}
        >
          {inDefault ? '已收藏' : '收藏'}
        </Button>
        <span className="text-xs text-gray-500 dark:text-gray-400">
          {inDefault ? '已加入默认收藏夹' : '加入默认收藏夹'}
        </span>
      </div>

      <details className="text-sm">
        <summary className="cursor-pointer text-gray-600 dark:text-gray-300">加入指定夹</summary>
        {custom.length === 0 ? (
          <p className="mt-2 text-sm text-gray-500 dark:text-gray-400">
            还没有自定义收藏夹。
            <Link className="ml-2 underline" to="/me/collections">
              去新建
            </Link>
          </p>
        ) : (
          <ul className="mt-2 list-none space-y-1 p-0">
            {custom.map((item) => (
              <li key={item.id}>
                <Checkbox
                  label={item.name}
                  checked={status.data?.collectionIds.includes(item.id) ?? false}
                  indeterminate={false}
                  disabled={busy}
                  onChange={(event) => {
                    if (event.target.checked) add.mutate(item.id);
                    else remove.mutate(item.id);
                  }}
                />
              </li>
            ))}
          </ul>
        )}
      </details>

      {error ? (
        <p role="alert" className="text-sm text-red-600 dark:text-red-400">
          {error}
        </p>
      ) : null}
    </section>
  );
}
