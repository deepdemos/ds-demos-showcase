import { useState } from 'react';
import { ThemeToggle, type Theme } from './theme-toggle';

export function ThemeToggleDemo() {
  const [theme, setTheme] = useState<Theme>('light');

  return (
    <ThemeToggle
      theme={theme}
      onToggle={() => setTheme((current) => (current === 'light' ? 'dark' : 'light'))}
    />
  );
}
