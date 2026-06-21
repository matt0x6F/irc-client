import { create } from 'zustand';
import { GetSetting, SetSetting } from '../../wailsjs/go/main/App';
import { EventsOn } from '../../wailsjs/runtime/runtime';

// Keys in the backend settings(key, value) table. Kept stable — renaming one would
// orphan the previously-persisted value.
const CONSOLIDATE_JOIN_QUIT_KEY = 'consolidateJoinQuit';
const PREFIX_DISPLAY_MODE_KEY = 'prefixDisplayMode';
const UPDATE_CHANNEL_KEY = 'updateChannel';

// How a user's channel-membership prefixes are shown in the nick list:
// 'icon' shows a single icon for their highest role; 'text' shows the full
// prefix string (e.g. "@+"), surfacing every role granted via multi-prefix.
export type PrefixDisplayMode = 'icon' | 'text';

// Which GitHub release channel the in-app updater tracks: 'stable' follows
// published releases only (the default), 'prerelease' also picks up the
// per-merge builds auto-published from main. The backend reads this once at
// startup (see updateChannelPrerelease in app_updater.go); the Wails updater
// can't be reconfigured live, so a change only takes effect after a restart.
export type UpdateChannel = 'stable' | 'prerelease';

// Narrowing guard for the persisted string, which GetSetting returns as a bare
// string (and '' for an unset key).
function isUpdateChannel(value: string): value is UpdateChannel {
  return value === 'stable' || value === 'prerelease';
}

interface SettingsState {
  consolidateJoinQuit: boolean;
  setConsolidateJoinQuit: (value: boolean) => void;
  prefixDisplayMode: PrefixDisplayMode;
  setPrefixDisplayMode: (value: PrefixDisplayMode) => void;
  updateChannel: UpdateChannel;
  setUpdateChannel: (value: UpdateChannel) => void;
}

/**
 * App-wide UI preferences backed by the durable SQLite settings table (via the
 * Wails App.GetSetting / App.SetSetting bindings), replacing the WKWebView
 * localStorage that macOS drops across restarts.
 *
 * Mirrors the theme store: the store carries a synchronous default for first
 * paint, initSettings() hydrates the real value asynchronously once the backend
 * is reachable, and the setter writes through to the backend. Components
 * subscribe to slices, so toggling the preference in Settings updates the
 * message view live — no localStorage poll / storage event needed.
 */
export const useSettingsStore = create<SettingsState>((set) => ({
  consolidateJoinQuit: false, // sensible default until initSettings() hydrates
  setConsolidateJoinQuit: (value) => {
    // Optimistically update the UI, then persist. A failed write logs but leaves
    // the in-memory value so the toggle still feels responsive.
    set({ consolidateJoinQuit: value });
    SetSetting(CONSOLIDATE_JOIN_QUIT_KEY, value ? 'true' : 'false').catch((error) => {
      console.error('Failed to persist consolidateJoinQuit:', error);
    });
  },
  prefixDisplayMode: 'icon', // sensible default until initSettings() hydrates
  setPrefixDisplayMode: (value) => {
    set({ prefixDisplayMode: value });
    SetSetting(PREFIX_DISPLAY_MODE_KEY, value).catch((error) => {
      console.error('Failed to persist prefixDisplayMode:', error);
    });
  },
  updateChannel: 'stable', // safe default until initSettings() hydrates
  setUpdateChannel: (value) => {
    set({ updateChannel: value });
    SetSetting(UPDATE_CHANNEL_KEY, value).catch((error) => {
      console.error('Failed to persist updateChannel:', error);
    });
  },
}));

/**
 * Hydrate persisted UI preferences from the backend into the store. Call once at
 * startup. On failure the synchronous defaults are kept.
 */
export async function initSettings(): Promise<void> {
  try {
    const [consolidate, prefixMode, channel] = await Promise.all([
      GetSetting(CONSOLIDATE_JOIN_QUIT_KEY),
      GetSetting(PREFIX_DISPLAY_MODE_KEY),
      GetSetting(UPDATE_CHANNEL_KEY),
    ]);
    useSettingsStore.setState({
      consolidateJoinQuit: consolidate === 'true',
      ...(prefixMode === 'text' || prefixMode === 'icon'
        ? { prefixDisplayMode: prefixMode }
        : {}),
      ...(isUpdateChannel(channel) ? { updateChannel: channel } : {}),
    });
  } catch (error) {
    console.error('Failed to load settings:', error);
  }

  // Reconcile when changed from another window (e.g. toggled in the standalone
  // Settings window → the main window's message view updates live). In-memory
  // only — never writes back, so there's no loop.
  EventsOn('setting:changed', (payload: { key: string; value: string }) => {
    if (payload.key === CONSOLIDATE_JOIN_QUIT_KEY) {
      useSettingsStore.setState({ consolidateJoinQuit: payload.value === 'true' });
    } else if (payload.key === PREFIX_DISPLAY_MODE_KEY) {
      if (payload.value === 'text' || payload.value === 'icon') {
        useSettingsStore.setState({ prefixDisplayMode: payload.value });
      }
    } else if (payload.key === UPDATE_CHANNEL_KEY) {
      if (isUpdateChannel(payload.value)) {
        useSettingsStore.setState({ updateChannel: payload.value });
      }
    }
  });
}
