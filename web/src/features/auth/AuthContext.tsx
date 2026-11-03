import { createContext, useCallback, useContext, useMemo, type ReactNode } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { getMe, postAuthLogout, type Me } from '@/api';

const ME_KEY = ['me'] as const;

/**
 * 读当前会话。
 *
 * 401 不是错误，是「没有登录」这一个正常状态——所以它映射成 `null` 而不是抛。
 * 把它当异常会让每个页面都得包一层 try/catch 才能渲染。
 * 其余状态码（5xx、网关错误）才真的抛：那时候「没登录」这个结论不可信，
 * 界面上必须说「不知道」，不能让用户以为自己是登出的。
 */
async function fetchMe(): Promise<Me | null> {
  const response = await getMe();
  if (response.status === 200) return response.data as Me;
  if (response.status === 401) return null;
  throw new Error(`无法确认登录状态（HTTP ${response.status}）`);
}

type AuthValue = {
  me: Me | null;
  isLoading: boolean;
  isError: boolean;
  /** 登录 / 注册成功后把会话写进缓存，省掉一次额外的 /me 往返 */
  setMe: (me: Me | null) => void;
  signOut: () => Promise<void>;
};

const AuthContext = createContext<AuthValue | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  const queryClient = useQueryClient();
  const { data, isPending, isError } = useQuery({
    queryKey: ME_KEY,
    queryFn: fetchMe,
    // 会话状态不能用 30 秒的全局 staleTime：登出后另一个标签页应该很快发现。
    staleTime: 0,
    retry: false,
  });

  const setMe = useCallback(
    (me: Me | null) => {
      queryClient.setQueryData(ME_KEY, me);
    },
    [queryClient],
  );

  const signOut = useCallback(async () => {
    // 先请求再清缓存：服务端吊销失败时界面仍然显示已登出，
    // 但下次刷新会回到登录态——比「界面说登出了、服务端还认」安全。
    await postAuthLogout();
    setMe(null);
    // 会话相关的查询（如个人数据）一并作废，避免换个账号看到上一个人的缓存。
    await queryClient.invalidateQueries();
  }, [queryClient, setMe]);

  const value = useMemo<AuthValue>(
    () => ({
      me: data ?? null,
      isLoading: isPending,
      isError,
      setMe,
      signOut,
    }),
    [data, isPending, isError, setMe, signOut],
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth(): AuthValue {
  const value = useContext(AuthContext);
  if (!value) throw new Error('useAuth 必须在 AuthProvider 内使用');
  return value;
}
