import { Skeleton } from './skeleton';

export function SkeletonDefaultDemo() {
  return <Skeleton />;
}

export function SkeletonCardDemo() {
  return <Skeleton lines={5} lineHeight="h-6" />;
}
