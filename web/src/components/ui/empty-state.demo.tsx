import { EmptyState } from './empty-state';
import { Button } from './button';

export function EmptyStateDefaultDemo() {
  return <EmptyState title="还没有作品" description="发布第一个作品后，它会出现在这里。" />;
}

export function EmptyStateWithActionDemo() {
  return (
    <EmptyState
      title="没有匹配的作品"
      description="试试换一个关键词，或者清空筛选条件。"
      action={<Button>清空筛选</Button>}
    />
  );
}
