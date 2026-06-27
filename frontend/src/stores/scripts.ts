import { create } from 'zustand';
import {
  ListScripts,
  EnableScript,
  DisableScript,
  ReloadScript,
  NewScript,
  OpenScriptsDir,
} from '../../wailsjs/go/main/App';
import { EventsOn } from '../../wailsjs/runtime/runtime';
import { main } from '../../wailsjs/go/models';

interface ScriptsState {
  scripts: main.ScriptInfo[];
  loading: boolean;
  // IDs of scripts with an in-flight enable/disable/reload action (per-row spinner).
  busy: Set<string>;
  // Path of the script just created via create(); shown inline with a "Reveal" button.
  lastCreatedPath: string | null;
  error: string | null;
  fetch: () => Promise<void>;
  enable: (id: string) => Promise<void>;
  disable: (id: string) => Promise<void>;
  reload: (id: string) => Promise<void>;
  create: (name: string) => Promise<void>;
  openDir: () => Promise<void>;
  clearCreated: () => void;
}

// markBusy / clearBusy keep the per-row in-flight set immutable for Zustand.
function withBusy(busy: Set<string>, id: string, on: boolean): Set<string> {
  const next = new Set(busy);
  if (on) next.add(id);
  else next.delete(id);
  return next;
}

/**
 * Inventory + lifecycle actions for in-process Cascade scripts, backed by the
 * PR9 Wails API. Mirrors the settings store: components subscribe to slices and
 * a single `script-lifecycle` subscription (registered by initScripts) refetches
 * the inventory whenever the backend changes a script's status — including the
 * watchdog auto-disabling a runaway script with no user action.
 */
export const useScriptsStore = create<ScriptsState>((set, get) => ({
  scripts: [],
  loading: false,
  busy: new Set(),
  lastCreatedPath: null,
  error: null,

  fetch: async () => {
    set({ loading: true });
    try {
      const list = await ListScripts();
      set({ scripts: list ?? [], loading: false });
    } catch (e) {
      console.error('Failed to list scripts:', e);
      set({ scripts: [], loading: false, error: String(e) });
    }
  },

  enable: async (id) => {
    set((s) => ({ busy: withBusy(s.busy, id, true), error: null }));
    try {
      await EnableScript(id);
    } catch (e) {
      console.error('Failed to enable script:', e);
      set({ error: String(e) });
    } finally {
      set((s) => ({ busy: withBusy(s.busy, id, false) }));
    }
    await get().fetch();
  },

  disable: async (id) => {
    set((s) => ({ busy: withBusy(s.busy, id, true), error: null }));
    try {
      await DisableScript(id);
    } catch (e) {
      console.error('Failed to disable script:', e);
      set({ error: String(e) });
    } finally {
      set((s) => ({ busy: withBusy(s.busy, id, false) }));
    }
    await get().fetch();
  },

  reload: async (id) => {
    set((s) => ({ busy: withBusy(s.busy, id, true), error: null }));
    try {
      await ReloadScript(id);
    } catch (e) {
      console.error('Failed to reload script:', e);
      set({ error: String(e) });
    } finally {
      set((s) => ({ busy: withBusy(s.busy, id, false) }));
    }
    await get().fetch();
  },

  create: async (name) => {
    set({ error: null });
    try {
      const path = await NewScript(name);
      set({ lastCreatedPath: path });
    } catch (e) {
      console.error('Failed to create script:', e);
      set({ error: String(e) });
    }
    await get().fetch();
  },

  openDir: async () => {
    try {
      await OpenScriptsDir();
    } catch (e) {
      console.error('Failed to open scripts dir:', e);
      set({ error: String(e) });
    }
  },

  clearCreated: () => set({ lastCreatedPath: null }),
}));

let subscribed = false;

/**
 * Register the one-time `script-lifecycle` subscription and do an initial fetch.
 * Idempotent — safe to call from multiple windows / mounts. Call once at startup
 * (main.tsx) alongside initSettings().
 */
export function initScripts(): void {
  void useScriptsStore.getState().fetch();
  if (subscribed) return;
  subscribed = true;
  // Refetch on every backend status change (enable/disable/reload/runaway).
  EventsOn('script-lifecycle', () => {
    void useScriptsStore.getState().fetch();
  });
}
