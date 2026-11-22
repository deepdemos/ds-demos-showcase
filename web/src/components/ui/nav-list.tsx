import { NavLink } from 'react-router';

export interface NavListItem {
  id: string;
  label: string;
  /** 给内部路由用；与 onSelect 二选一。 */
  to?: string;
  onSelect?: () => void;
  badge?: number;
  /** 精确匹配；用于 /me 这类有子路径的入口。 */
  end?: boolean;
}

export interface NavListProps {
  items: readonly NavListItem[];
  ariaLabel: string;
  orientation?: 'horizontal' | 'vertical';
  /** 非链接项的当前项；链接项由 NavLink 自己判断。 */
  activeId?: string;
  /** 点击任一条目后的回调；用于关闭移动端菜单等页面级副作用。 */
  onNavigate?: (id: string) => void;
  className?: string;
}

/**
 * 站点导航列表。
 *
 * 为什么不用 Ring UI 现成的 tabs：这里要同时承载「路由链接」与「页内按钮」
 * 两种条目，而且 /me 与 /admin 的二级导航需要竖排。保留一个受控的 activeId
 * 给 Dev Room 演示用，真实页面交给 NavLink 的 aria-current。
 */
export function NavList({
  items,
  ariaLabel,
  orientation = 'horizontal',
  activeId,
  onNavigate,
  className = '',
}: NavListProps) {
  const listClass =
    orientation === 'horizontal' ? 'flex flex-wrap items-center gap-1' : 'flex flex-col gap-0.5';
  const itemClass = (active: boolean) => {
    const base =
      'inline-flex items-center gap-1.5 whitespace-nowrap px-2 py-2 text-sm no-underline transition-colors';
    if (orientation === 'vertical') {
      return [
        base,
        'border-l-2',
        active
          ? 'border-gray-900 bg-gray-50 font-medium text-gray-900'
          : 'border-transparent text-gray-600 hover:bg-gray-50 hover:text-gray-900',
      ].join(' ');
    }
    return [
      base,
      'border-b-2',
      active
        ? 'border-gray-900 font-medium text-gray-900'
        : 'border-transparent text-gray-600 hover:text-gray-900',
    ].join(' ');
  };

  return (
    <nav aria-label={ariaLabel} className={className}>
      <ul className={listClass + ' m-0 list-none p-0'}>
        {items.map((item) => {
          const content = (
            <>
              {item.label}
              {item.badge !== undefined && item.badge > 0 ? (
                <span className="rounded-full bg-gray-200 px-1.5 text-xs tabular-nums text-gray-700">
                  {item.badge > 99 ? '99+' : item.badge}
                </span>
              ) : null}
            </>
          );
          return (
            <li key={item.id} className="m-0">
              {item.to ? (
                <NavLink
                  to={item.to}
                  end={item.end}
                  onClick={() => onNavigate?.(item.id)}
                  className={({ isActive }) => itemClass(isActive)}
                >
                  {content}
                </NavLink>
              ) : (
                <button
                  type="button"
                  onClick={() => {
                    onNavigate?.(item.id);
                    item.onSelect?.();
                  }}
                  className={itemClass(activeId === item.id)}
                  aria-current={activeId === item.id ? 'page' : undefined}
                >
                  {content}
                </button>
              )}
            </li>
          );
        })}
      </ul>
    </nav>
  );
}
