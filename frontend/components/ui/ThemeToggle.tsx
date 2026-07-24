'use client';

import { useTheme } from '@/components/ui/theme';

export function ThemeToggle() {
  const { theme, toggleTheme } = useTheme();
  return (
    <button
      onClick={toggleTheme}
      aria-label="Toggle theme"
      className="text-chrome-fg px-2 py-1 rounded hover:bg-white/10"
    >
      {theme === 'light' ? '🌙' : '☀️'}
    </button>
  );
}
