import { useEffect, useState } from 'react';

export type ThemeMode = 'dark' | 'light';

const STORAGE_KEY = 'xissue_theme';

function getInitialTheme(): ThemeMode {
  try {
    const saved = localStorage.getItem(STORAGE_KEY);
    if (saved === 'light' || saved === 'dark') {
      return saved;
    }
  } catch {
    // ignore
  }
  if (typeof window !== 'undefined' && window.matchMedia?.('(prefers-color-scheme: light)').matches) {
    return 'light';
  }
  return 'dark';
}

let currentTheme: ThemeMode = getInitialTheme();
const listeners = new Set<(t: ThemeMode) => void>();

function applyTheme(t: ThemeMode) {
  currentTheme = t;
  if (typeof document !== 'undefined') {
    document.documentElement.setAttribute('data-theme', t);
  }
  try {
    localStorage.setItem(STORAGE_KEY, t);
  } catch {
    // ignore
  }
  listeners.forEach((fn) => fn(t));
}

// Apply immediately on module load
applyTheme(currentTheme);

export function useTheme(): [ThemeMode, () => void, (t: ThemeMode) => void] {
  const [theme, setThemeState] = useState<ThemeMode>(currentTheme);
  useEffect(() => {
    const fn = (t: ThemeMode) => setThemeState(t);
    listeners.add(fn);
    return () => {
      listeners.delete(fn);
    };
  }, []);
  const toggle = () => applyTheme(currentTheme === 'dark' ? 'light' : 'dark');
  return [theme, toggle, applyTheme];
}
