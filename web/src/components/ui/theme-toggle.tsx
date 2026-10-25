import { Button } from './button';

export type Theme = 'light' | 'dark';

export interface ThemeToggleProps {
  theme: Theme;
  onToggle: () => void;
}

export function ThemeToggle({ theme, onToggle }: ThemeToggleProps) {
  const isDark = theme === 'dark';

  return (
    <Button
      onClick={onToggle}
      aria-label={isDark ? '切换到浅色模式' : '切换到深色模式'}
      aria-pressed={isDark}
    >
      {isDark ? '浅色模式' : '深色模式'}
    </Button>
  );
}
