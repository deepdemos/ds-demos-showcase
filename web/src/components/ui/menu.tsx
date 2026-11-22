import { useEffect, useId, useRef, useState } from 'react';
import { Link } from 'react-router';

export interface MenuItem {
  id: string;
  label: string;
  to?: string;
  onSelect?: () => void;
  badge?: number;
  danger?: boolean;
}

export interface MenuProps {
  /** 触发按钮上的文字（用户名 / 更多）。 */
  label: string;
  items: readonly MenuItem[];
  ariaLabel: string;
  align?: 'left' | 'right';
  /** 打开/关闭时通知调用方；用于关闭移动端菜单等页面级副作用。 */
  onOpenChange?: (open: boolean) => void;
}

/**
 * 用户菜单。
 *
 * 只做三件事：打开/关闭、键盘关闭、点外面关闭。菜单项可以是路由链接，
 * 也可以是动作（登出）。不引入 Ring UI 的 Dropdown，是因为这里需要
 * 完全可控的焦点顺序与 aria 属性；样式仍然保持中性。
 */
export function Menu({ label, items, ariaLabel, align = 'right', onOpenChange }: MenuProps) {
  const [open, setOpen] = useState(false);
  const rootRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const menuId = useId();

  useEffect(() => {
    if (!open) return;
    const close = () => {
      setOpen(false);
      onOpenChange?.(false);
    };
    const onPointerDown = (event: MouseEvent) => {
      if (!rootRef.current?.contains(event.target as Node)) close();
    };
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== 'Escape') return;
      close();
      triggerRef.current?.focus();
    };
    document.addEventListener('mousedown', onPointerDown);
    document.addEventListener('keydown', onKeyDown);
    return () => {
      document.removeEventListener('mousedown', onPointerDown);
      document.removeEventListener('keydown', onKeyDown);
    };
  }, [open, onOpenChange]);

  useEffect(() => {
    if (!open) return;
    const first = rootRef.current?.querySelector<HTMLElement>('[role="menuitem"]');
    first?.focus();
  }, [open]);

  const close = () => {
    setOpen(false);
    onOpenChange?.(false);
  };

  return (
    <div className="relative" ref={rootRef}>
      <button
        ref={triggerRef}
        type="button"
        aria-haspopup="menu"
        aria-expanded={open}
        aria-controls={menuId}
        data-testid="user-menu-trigger"
        onClick={() => {
          const next = !open;
          setOpen(next);
          onOpenChange?.(next);
        }}
        className="max-w-40 truncate rounded border border-gray-300 bg-white px-3 py-1.5 text-sm text-gray-800 hover:border-gray-400"
      >
        {label}
      </button>
      {open ? (
        <div
          id={menuId}
          role="menu"
          aria-label={ariaLabel}
          className={[
            'absolute z-20 mt-1 min-w-48 rounded border border-gray-200 bg-white py-1 shadow-sm',
            align === 'right' ? 'right-0' : 'left-0',
          ].join(' ')}
        >
          {items.map((item) =>
            item.to ? (
              <Link
                key={item.id}
                to={item.to}
                role="menuitem"
                onClick={close}
                className="flex items-center justify-between gap-3 px-3 py-2 text-sm text-gray-700 no-underline hover:bg-gray-50"
              >
                <span>{item.label}</span>
                {item.badge !== undefined && item.badge > 0 ? (
                  <span className="rounded-full bg-gray-200 px-1.5 text-xs tabular-nums text-gray-700">
                    {item.badge > 99 ? '99+' : item.badge}
                  </span>
                ) : null}
              </Link>
            ) : (
              <button
                key={item.id}
                type="button"
                role="menuitem"
                onClick={() => {
                  close();
                  item.onSelect?.();
                }}
                className={[
                  'block w-full px-3 py-2 text-left text-sm hover:bg-gray-50',
                  item.danger ? 'text-red-700' : 'text-gray-700',
                ].join(' ')}
              >
                {item.label}
              </button>
            ),
          )}
        </div>
      ) : null}
    </div>
  );
}
