import { useState } from 'react';
import { Menu } from './menu';

export function MenuDefaultDemo() {
  const [last, setLast] = useState('还没有选择');
  return (
    <div className="flex flex-col items-end gap-2">
      <Menu
        ariaLabel="示例菜单"
        label="更多"
        items={[
          { id: 'profile', label: '资料设置', onSelect: () => setLast('资料设置') },
          { id: 'prefs', label: '偏好设置', onSelect: () => setLast('偏好设置') },
          { id: 'logout', label: '登出', danger: true, onSelect: () => setLast('登出') },
        ]}
      />
      <span className="text-xs text-gray-500">{last}</span>
    </div>
  );
}
