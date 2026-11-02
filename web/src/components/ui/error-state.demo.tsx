import { ErrorState } from './error-state';

export function ErrorStateDefaultDemo() {
  return <ErrorState title="加载失败" description="网络似乎不太通，稍后再试一次。" />;
}

export function ErrorStateWithRetryDemo() {
  return (
    <ErrorState title="加载失败" description="网络似乎不太通，稍后再试一次。" onRetry={() => {}} />
  );
}
