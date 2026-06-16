import { create } from 'zustand';

// Composer/UI preferences persisted to localStorage. Mirrors stores/theme.ts:
// load-with-try/catch + write-through setter, so the value is shared live
// between the composer's "Aa" toggle and the Settings → Display switch.

const SHOW_FORMATTING_TOOLBAR_KEY = 'cascade-chat-show-formatting-toolbar';

function loadShowFormattingToolbar(): boolean {
  try {
    const v = localStorage.getItem(SHOW_FORMATTING_TOOLBAR_KEY);
    if (v === 'true') return true;
    if (v === 'false') return false;
  } catch {
    /* ignore */
  }
  return true; // default: shown, so the feature is discoverable
}

interface PreferencesState {
  showFormattingToolbar: boolean;
  setShowFormattingToolbar: (show: boolean) => void;
  toggleFormattingToolbar: () => void;
}

export const usePreferencesStore = create<PreferencesState>((set, get) => ({
  showFormattingToolbar: loadShowFormattingToolbar(),
  setShowFormattingToolbar: (show) => {
    try {
      localStorage.setItem(SHOW_FORMATTING_TOOLBAR_KEY, show ? 'true' : 'false');
    } catch {
      /* ignore */
    }
    set({ showFormattingToolbar: show });
  },
  toggleFormattingToolbar: () => get().setShowFormattingToolbar(!get().showFormattingToolbar),
}));
