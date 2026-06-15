import { create } from 'zustand';

export type ThemeMode = 'light' | 'dark' | 'system';
export type Accent = 'blue' | 'purple' | 'emerald' | 'rose' | 'amber';

export const ACCENTS: { id: Accent; label: string; swatch: string }[] = [
  { id: 'blue', label: 'Blue', swatch: '#2563eb' },
  { id: 'purple', label: 'Purple', swatch: '#7c3aed' },
  { id: 'emerald', label: 'Emerald', swatch: '#059669' },
  { id: 'rose', label: 'Rose', swatch: '#e11d48' },
  { id: 'amber', label: 'Amber', swatch: '#d97706' },
];

const MODE_KEY = 'cascade-chat-theme-mode';
const ACCENT_KEY = 'cascade-chat-theme-accent';

const isAccent = (v: string | null): v is Accent =>
  v === 'blue' || v === 'purple' || v === 'emerald' || v === 'rose' || v === 'amber';
const isMode = (v: string | null): v is ThemeMode =>
  v === 'light' || v === 'dark' || v === 'system';

function loadMode(): ThemeMode {
  try {
    const v = localStorage.getItem(MODE_KEY);
    if (isMode(v)) return v;
  } catch {
    /* ignore */
  }
  return 'system';
}

function loadAccent(): Accent {
  try {
    const v = localStorage.getItem(ACCENT_KEY);
    if (isAccent(v)) return v;
  } catch {
    /* ignore */
  }
  return 'blue';
}

function prefersDark(): boolean {
  return typeof window !== 'undefined' &&
    window.matchMedia?.('(prefers-color-scheme: dark)').matches;
}

/** Resolve the effective dark/light from a mode, then apply to <html>. */
export function applyTheme(mode: ThemeMode, accent: Accent): void {
  const root = document.documentElement;
  const isDark = mode === 'dark' || (mode === 'system' && prefersDark());
  root.classList.toggle('dark', isDark);
  root.setAttribute('data-accent', accent);
}

interface ThemeState {
  mode: ThemeMode;
  accent: Accent;
  setMode: (mode: ThemeMode) => void;
  setAccent: (accent: Accent) => void;
}

export const useThemeStore = create<ThemeState>((set, get) => ({
  mode: loadMode(),
  accent: loadAccent(),
  setMode: (mode) => {
    try {
      localStorage.setItem(MODE_KEY, mode);
    } catch {
      /* ignore */
    }
    applyTheme(mode, get().accent);
    set({ mode });
  },
  setAccent: (accent) => {
    try {
      localStorage.setItem(ACCENT_KEY, accent);
    } catch {
      /* ignore */
    }
    applyTheme(get().mode, accent);
    set({ accent });
  },
}));

/**
 * Apply the persisted theme to <html> immediately, and keep "system" mode in
 * sync with the OS preference. Call once, before React renders, to avoid a
 * flash of the wrong theme.
 */
export function initTheme(): void {
  const mode = loadMode();
  const accent = loadAccent();
  applyTheme(mode, accent);

  window.matchMedia?.('(prefers-color-scheme: dark)').addEventListener('change', () => {
    // Only react when following the system; explicit light/dark are untouched.
    if (useThemeStore.getState().mode === 'system') {
      applyTheme('system', useThemeStore.getState().accent);
    }
  });
}
