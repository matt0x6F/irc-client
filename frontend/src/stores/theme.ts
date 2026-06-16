import { create } from 'zustand';
import { GetSetting, SetSetting } from '../../wailsjs/go/main/App';
import { EventsOn } from '../../wailsjs/runtime/runtime';

export type ThemeMode = 'light' | 'dark' | 'system';
export type Accent = 'blue' | 'purple' | 'emerald' | 'rose' | 'amber';

export const ACCENTS: { id: Accent; label: string; swatch: string }[] = [
  { id: 'blue', label: 'Blue', swatch: '#2563eb' },
  { id: 'purple', label: 'Purple', swatch: '#7c3aed' },
  { id: 'emerald', label: 'Emerald', swatch: '#059669' },
  { id: 'rose', label: 'Rose', swatch: '#e11d48' },
  { id: 'amber', label: 'Amber', swatch: '#d97706' },
];

// Keys in the backend settings store. Theme prefs used to live in the
// WKWebView's localStorage, but macOS does not persist that across app
// restarts (the theme reset on every launch), so they are now stored in the
// SQLite DB via the Go backend.
const MODE_KEY = 'theme.mode';
const ACCENT_KEY = 'theme.accent';

const DEFAULT_MODE: ThemeMode = 'system';
const DEFAULT_ACCENT: Accent = 'blue';

const isAccent = (v: string | null): v is Accent =>
  v === 'blue' || v === 'purple' || v === 'emerald' || v === 'rose' || v === 'amber';
const isMode = (v: string | null): v is ThemeMode =>
  v === 'light' || v === 'dark' || v === 'system';

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
  mode: DEFAULT_MODE,
  accent: DEFAULT_ACCENT,
  setMode: (mode) => {
    // Apply immediately for a responsive UI, then persist in the background.
    // The change is already live for this session even if the write fails.
    applyTheme(mode, get().accent);
    set({ mode });
    void SetSetting(MODE_KEY, mode).catch((e) =>
      console.error('Failed to persist theme mode', e),
    );
  },
  setAccent: (accent) => {
    applyTheme(get().mode, accent);
    set({ accent });
    void SetSetting(ACCENT_KEY, accent).catch((e) =>
      console.error('Failed to persist accent', e),
    );
  },
}));

/**
 * Load the persisted theme from the backend and apply it to <html>. Returns a
 * promise so the caller can await it before first paint to avoid a flash of the
 * wrong theme. A synchronous default is applied first, so the page is never
 * unstyled even if the backend read is slow or fails.
 *
 * Call once, before React renders.
 */
export async function initTheme(): Promise<void> {
  // Synchronous default so the very first paint is themed no matter what.
  applyTheme(DEFAULT_MODE, DEFAULT_ACCENT);

  try {
    const [rawMode, rawAccent] = await Promise.all([
      GetSetting(MODE_KEY),
      GetSetting(ACCENT_KEY),
    ]);
    const mode = isMode(rawMode) ? rawMode : DEFAULT_MODE;
    const accent = isAccent(rawAccent) ? rawAccent : DEFAULT_ACCENT;
    useThemeStore.setState({ mode, accent });
    applyTheme(mode, accent);
  } catch (e) {
    // Keep the defaults already applied above.
    console.error('Failed to load persisted theme; using defaults', e);
  }

  window.matchMedia?.('(prefers-color-scheme: dark)').addEventListener('change', () => {
    // Only react when following the system; explicit light/dark are untouched.
    if (useThemeStore.getState().mode === 'system') {
      applyTheme('system', useThemeStore.getState().accent);
    }
  });

  // Reconcile when theme is changed from another window (e.g. the standalone
  // Settings window changes the accent → the main window re-themes live). This
  // only updates in-memory state + re-applies to <html>; it must never call
  // SetSetting back, or it would loop. The echo to the originating window is
  // idempotent (same value, same DOM write).
  EventsOn('setting:changed', (payload: { key: string; value: string }) => {
    if (payload.key === MODE_KEY && isMode(payload.value)) {
      useThemeStore.setState({ mode: payload.value });
      applyTheme(payload.value, useThemeStore.getState().accent);
    } else if (payload.key === ACCENT_KEY && isAccent(payload.value)) {
      useThemeStore.setState({ accent: payload.value });
      applyTheme(useThemeStore.getState().mode, payload.value);
    }
  });
}
