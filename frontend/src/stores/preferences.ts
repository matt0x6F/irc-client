import { create } from 'zustand';
import { GetSetting, SetSetting } from '../../wailsjs/go/main/App';
import { EventsOn } from '../../wailsjs/runtime/runtime';

// Composer/UI preferences backed by the durable SQLite settings table (via the
// Wails App.GetSetting / App.SetSetting bindings). This used to live in
// WKWebView localStorage, but macOS drops that across restarts AND it can't be
// shared with the standalone Settings window (a separate webview = a separate
// localStorage). Routing it through the backend fixes both: it survives
// restarts, and a change in the Settings window reaches the main window's
// composer live via the setting:changed broadcast.
//
// Note the key differs from the old localStorage key — the previous value isn't
// migrated (macOS rarely persisted it anyway); the default is restored on first
// run after the switch.
const SHOW_FORMATTING_TOOLBAR_KEY = 'showFormattingToolbar';
const DEFAULT_SHOW_FORMATTING_TOOLBAR = true; // shown, so the feature is discoverable

const HELP_DISPLAY_MODE_KEY = 'help.display_mode';
type HelpDisplayMode = 'dialog' | 'buffer';
const DEFAULT_HELP_DISPLAY_MODE: HelpDisplayMode = 'dialog';

const CLOSE_BUFFER_ON_LEAVE_KEY = 'closeBufferOnLeave';
const DEFAULT_CLOSE_BUFFER_ON_LEAVE = true; // leaving a channel also closes its buffer

interface PreferencesState {
  showFormattingToolbar: boolean;
  setShowFormattingToolbar: (show: boolean) => void;
  toggleFormattingToolbar: () => void;
  helpDisplayMode: HelpDisplayMode;
  setHelpDisplayMode: (mode: HelpDisplayMode) => void;
  closeBufferOnLeave: boolean;
  setCloseBufferOnLeave: (close: boolean) => void;
}

export const usePreferencesStore = create<PreferencesState>((set, get) => ({
  showFormattingToolbar: DEFAULT_SHOW_FORMATTING_TOOLBAR, // until initPreferences() hydrates
  setShowFormattingToolbar: (show) => {
    // Optimistic update, then persist. A failed write logs but leaves the
    // in-memory value so the toggle stays responsive.
    set({ showFormattingToolbar: show });
    SetSetting(SHOW_FORMATTING_TOOLBAR_KEY, show ? 'true' : 'false').catch((error) => {
      console.error('Failed to persist showFormattingToolbar:', error);
    });
  },
  toggleFormattingToolbar: () => get().setShowFormattingToolbar(!get().showFormattingToolbar),
  helpDisplayMode: DEFAULT_HELP_DISPLAY_MODE,
  setHelpDisplayMode: (mode) => {
    set({ helpDisplayMode: mode });
    SetSetting(HELP_DISPLAY_MODE_KEY, mode).catch((error) => {
      console.error('Failed to persist helpDisplayMode:', error);
    });
  },
  closeBufferOnLeave: DEFAULT_CLOSE_BUFFER_ON_LEAVE, // until initPreferences() hydrates
  setCloseBufferOnLeave: (close) => {
    set({ closeBufferOnLeave: close });
    SetSetting(CLOSE_BUFFER_ON_LEAVE_KEY, close ? 'true' : 'false').catch((error) => {
      console.error('Failed to persist closeBufferOnLeave:', error);
    });
  },
}));

/**
 * Hydrate the formatting-toolbar preference from the backend and subscribe to
 * cross-window changes. Call once at startup (in every window). On failure the
 * synchronous default is kept.
 */
export async function initPreferences(): Promise<void> {
  try {
    const value = await GetSetting(SHOW_FORMATTING_TOOLBAR_KEY);
    // GetSetting returns "" for an unset key — keep the default in that case.
    if (value === 'true' || value === 'false') {
      usePreferencesStore.setState({ showFormattingToolbar: value === 'true' });
    }
  } catch (error) {
    console.error('Failed to load preferences:', error);
  }

  try {
    const mode = await GetSetting(HELP_DISPLAY_MODE_KEY);
    if (mode === 'dialog' || mode === 'buffer') {
      usePreferencesStore.setState({ helpDisplayMode: mode });
    }
  } catch (error) {
    console.error('Failed to load help display mode:', error);
  }

  try {
    const value = await GetSetting(CLOSE_BUFFER_ON_LEAVE_KEY);
    if (value === 'true' || value === 'false') {
      usePreferencesStore.setState({ closeBufferOnLeave: value === 'true' });
    }
  } catch (error) {
    console.error('Failed to load closeBufferOnLeave:', error);
  }

  // Reconcile when the value is changed from another window. This only updates
  // in-memory state — it must never call SetSetting back, or it would loop.
  EventsOn('setting:changed', (payload: { key: string; value: string }) => {
    if (payload.key === SHOW_FORMATTING_TOOLBAR_KEY && (payload.value === 'true' || payload.value === 'false')) {
      usePreferencesStore.setState({ showFormattingToolbar: payload.value === 'true' });
    }
    if (payload.key === HELP_DISPLAY_MODE_KEY && (payload.value === 'dialog' || payload.value === 'buffer')) {
      usePreferencesStore.setState({ helpDisplayMode: payload.value });
    }
    if (payload.key === CLOSE_BUFFER_ON_LEAVE_KEY && (payload.value === 'true' || payload.value === 'false')) {
      usePreferencesStore.setState({ closeBufferOnLeave: payload.value === 'true' });
    }
  });
}
