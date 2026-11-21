import { useState } from 'react';
import { NavList } from './nav-list';

const ITEMS = [
  { id: 'demos', label: '作品库', onSelect: () => undefined },
  { id: 'rankings', label: '排行榜', onSelect: () => undefined },
  { id: 'submit', label: '投稿', onSelect: () => undefined },
];

export function NavListHorizontalDemo() {
  const [active, setActive] = useState('demos');
  return (
    <NavList
      ariaLabel="横向导航示例"
      activeId={active}
      items={ITEMS.map((item) => ({ ...item, onSelect: () => setActive(item.id) }))}
    />
  );
}

export function NavListVerticalDemo() {
  const [active, setActive] = useState('demos');
  return (
    <NavList
      ariaLabel="纵向导航示例"
      orientation="vertical"
      activeId={active}
      items={ITEMS.map((item) => ({ ...item, onSelect: () => setActive(item.id) }))}
    />
  );
}
