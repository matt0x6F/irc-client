# Cascade Scripting v1 — PR10: Frontend Scripts panel

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship the React Scripts management panel that consumes the frozen PR9 backend API (`ListScripts`/`EnableScript`/`DisableScript`/`ReloadScript`/`NewScript`/`OpenScriptsDir`) — inventory with live status, enable/disable, reload, "New script", error display — plus one small backend addition: emit a `script-lifecycle` Wails event on every status transition so the panel reflects watchdog auto-disables without user action.

**Architecture:** A dedicated `<ScriptsPanel/>` component backed by a new `stores/scripts.ts` Zustand store (not inline in `settings-panel.tsx`, unlike the Plugins pane). The store fetches `ListScripts()` and subscribes via `EventsOn('script-lifecycle', …)` to refetch — mirroring how `commands.ts` reacts to `plugin-lifecycle` and how `settings.ts` reacts to `setting:changed`. The backend emits `script-lifecycle` through a new nil-safe `Host.Notify` callback (sibling to the existing `Host.PersistEnabled`), wired in `app.go` to `app.emit(...)`; the `Manager` fires it at every status chokepoint, including the watchdog's runaway auto-disable. The panel is reachable via a new "Scripts" nav item in the settings window.

**Tech Stack:** Go (manager + Wails `emit`), React 19 + TypeScript, Zustand, Tailwind CSS v4, Vitest.

## Global Constraints

- **Base branch:** PR10 stacks on `cascade-scripting` (PR9 = #91). Branch from `origin/cascade-scripting`, NOT `main` — the PR9 backend code, bindings, and frontend stores only exist on that branch. PR target is `cascade-scripting`.
- **Module path lowercase** `github.com/matt0x6f/irc-client`.
- **Go tests:** `CGO_ENABLED=1 go test -tags fts5 ./internal/... -count=1`; `internal/script` also runs with `-race`. `gofmt -l` must be empty and `go vet` must pass — CI gates on both.
- **Frontend tests:** `cd frontend && npx vitest run`. The vitest CI job is known-flaky — rerun on a spurious failure before investigating.
- **No new Wails-bound methods.** PR9 already added and regenerated the `ListScripts`/`Enable`/`Disable`/`Reload`/`NewScript`/`OpenScriptsDir` bindings and the `ScriptInfo` model. `script-lifecycle` is a *runtime event* (via `app.emit`), so **do not** run `wails3 generate bindings` in this PR — no binding surface changes.
- **App `package main` needs a stub `frontend/dist`** to `go build`: `mkdir -p frontend/dist && printf '<!doctype html>' > frontend/dist/index.html` (git-ignored; never commit).
- **No `time.Time` across the Wails boundary** (not needed here; the event payload is a plain map).
- **Preserve PR4–9 behavior:** events, watchdog, timers, hot reload, persistence, and the PR9 lifecycle fixes are unchanged except for the additive `Notify` calls.

## Grounded facts

- **Bindings are already wired.** `frontend/wailsjs/go/main/App.ts` is a one-line `export * from '…/bindings/…/app'`, so `ListScripts`, `EnableScript`, `DisableScript`, `ReloadScript`, `NewScript`, `OpenScriptsDir` are importable from `'../../wailsjs/go/main/App'` today. `frontend/wailsjs/go/models.ts` re-exports `main`, so `main.ScriptInfo` is the typed row. Its fields: `id`, `name`, `description`, `status`, `enabled` (bool), `error`, `perms` (string[]) — all confirmed in `frontend/bindings/.../models.js`.
- **Status values** (`internal/extension/extension.go`): `"loaded"`, `"disabled"`, `"error"`, `"runaway"`.
- **Emit helper** (`app.go`): `func (a *App) emit(name string, data ...any)` → `a.app.Event.Emit(name, data...)`; nil-safe when `a.app == nil`. Precedent payload: `a.emit("plugin-lifecycle", map[string]interface{}{...})` (app_events.go).
- **Runtime event API** (`frontend/wailsjs/runtime/runtime.ts`): `EventsOn(name, (...data) => void): () => void` — returns an unsubscribe fn; v3 shim delivers the emitted payload to the callback. Precedent: `commands.ts` does `EventsOn('plugin-lifecycle', () => …)` and ignores the payload.
- **Manager status chokepoints** (`internal/script/manager.go`): user-driven `Disable`/`Enable`/`ReloadByID(→reload)`; background `disableScript` (watchdog runaway) and `handleFSEvent` (hot reload: `reload` on modify, `loadDir` on new dir, `unload` on delete). `reload`, `unload`, `Disable`, `Enable`, `disableScript` are the methods to instrument; `handleFSEvent`'s new-dir branch calls `loadDir` directly and also needs a notify.
- **Host struct** (`internal/script/manager.go`) already carries nil-safe callbacks `LoadDisabled` and `PersistEnabled`; `Notify` follows the same shape. `app.go` builds the `script.Host{…}` literal around lines 127–151.
- **Settings panel** (`frontend/src/components/settings-panel.tsx`): `export type SettingsSection = 'networks' | 'plugins' | 'display' | 'notifications' | 'advanced' | 'about'` (line 17) with an `isSettingsSection` guard (lines 19–20); `SettingsPanel({ section, onSectionChange })`; a `renderContent()` `switch (section)` with `case 'plugins':` (line 1070); and a `navItems` array (lines ~1518–1524). The standalone Settings window persists the last section and validates it with `isSettingsSection`, so a new section must be added to BOTH the type and the guard.
- **Store + init precedent** (`frontend/src/stores/settings.ts`): a `create<…>()` store with optimistic setters, plus an exported `initSettings()` that hydrates and registers `EventsOn(...)`. `initSettings()` is invoked once in `frontend/src/main.tsx` (line 76).
- **Store test precedent** (`frontend/src/stores/settings.notifications.test.ts`): `vi.mock('../../wailsjs/go/main/App', …)` and `vi.mock('../../wailsjs/runtime/runtime', () => ({ EventsOn: vi.fn() }))`, then drive the store via `useStore.getState()`.

---

## PR10 — File Structure

- `internal/script/manager.go` — add `Host.Notify func()`, a nil-safe `m.notify()` helper, and notify calls at the five status chokepoints + the hot-reload new-dir branch (Task 1).
- `internal/script/manager_test.go` — assert `Notify` fires on `Disable`/`Enable`/`disableScript`/`reload` (Task 1).
- `app.go` — extend the `script.Host{…}` literal with `Notify: func() { app.emit("script-lifecycle", …) }` (Task 1).
- `frontend/src/stores/scripts.ts` (new) — Zustand store + `initScripts()` (Task 2).
- `frontend/src/stores/scripts.test.ts` (new) — store + lifecycle-refetch tests (Task 2).
- `frontend/src/main.tsx` — call `initScripts()` alongside `initSettings()` (Task 2).
- `frontend/src/components/scripts-panel.tsx` (new) — the panel UI (Task 3).
- `frontend/src/components/__tests__/scripts-panel.test.tsx` (new) — render/interaction tests (Task 3).
- `frontend/src/components/settings-panel.tsx` — add `'scripts'` to the type + guard, a nav item, and a `case 'scripts'` render (Task 4).
- `frontend/src/components/__tests__/settings-panel.test.tsx` — assert the Scripts section renders the panel (Task 4).

---

## Task 1: Backend — emit `script-lifecycle` on status transitions

**Files:**
- Modify: `internal/script/manager.go` (Host struct ~line 35; `reload`, `unload`, `Disable`, `Enable`, `disableScript`, `handleFSEvent`)
- Test: `internal/script/manager_test.go`
- Modify: `app.go` (the `script.Host{…}` literal, ~lines 127–151)

**Interfaces:**
- Consumes: nothing new.
- Produces:
  - `Host.Notify func()` — nil-safe callback invoked after any change to the registry's script set or a script's status.
  - `func (m *Manager) notify()` — calls `m.host.Notify()` if non-nil.
  - Runtime event `"script-lifecycle"` (payload `map[string]any{}`, currently empty) emitted by `app.go`.

- [ ] **Step 1: Write the failing test**

Add to `internal/script/manager_test.go`. This drives a manager over a real fixture dir and counts `Notify` calls across the lifecycle methods. Use the existing test helpers/fixtures in that file as a model for `newTestManager`/temp-dir setup; the assertion below is the new behavior.

```go
func TestManagerNotifiesOnLifecycleTransitions(t *testing.T) {
	dir := t.TempDir()
	// A minimal valid script dir: <dir>/greeter/greeter.go exporting OnText.
	scriptDir := filepath.Join(dir, "greeter")
	if err := os.MkdirAll(scriptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := "package main\n\nimport \"github.com/matt0x6f/irc-client/cascade\"\n\nfunc OnText(e cascade.TextEvent) {}\n"
	if err := os.WriteFile(filepath.Join(scriptDir, "greeter.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	var notifies int
	bus := events.NewEventBus()
	m := NewManager(bus, dir, Host{
		Notify: func() { notifies++ },
	})
	if err := m.LoadAll(); err != nil {
		t.Fatalf("LoadAll: %v", err)
	}

	id := extension.ID("greeter")
	notifies = 0 // ignore any load-time notifies; measure explicit transitions

	m.Disable(id)
	if notifies == 0 {
		t.Fatalf("Disable did not notify")
	}
	notifies = 0

	m.Enable(id)
	if notifies == 0 {
		t.Fatalf("Enable did not notify")
	}
	notifies = 0

	m.ReloadByID(id)
	if notifies == 0 {
		t.Fatalf("ReloadByID did not notify")
	}
	notifies = 0

	m.disableScript(id, "test runaway")
	if notifies == 0 {
		t.Fatalf("disableScript (runaway) did not notify")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `CGO_ENABLED=1 go test -tags fts5 ./internal/script/ -run TestManagerNotifiesOnLifecycleTransitions -v`
Expected: FAIL — `unknown field 'Notify' in struct literal of type Host` (compile error).

- [ ] **Step 3: Add `Notify` to `Host` and the `notify()` helper**

In `internal/script/manager.go`, add the field to `Host` (after `PersistEnabled`):

```go
	// PersistEnabled persists an enable/disable toggle. Nil-safe.
	PersistEnabled func(id string, enabled bool)
	// Notify is called after any change to the loaded-script set or a script's
	// status (enable/disable/reload/runaway), so the frontend can refetch the
	// inventory. Nil-safe.
	Notify func()
```

Add the helper near the other small Manager methods (e.g. just below `registry()`):

```go
// notify signals the host that the script inventory or a script's status changed.
func (m *Manager) notify() {
	if m.host.Notify != nil {
		m.host.Notify()
	}
}
```

- [ ] **Step 4: Call `notify()` at every status chokepoint**

Add `m.notify()` as the last statement of `Disable`, `Enable`, `disableScript`, `reload`, and `unload`, and in the new-directory branch of `handleFSEvent`. Exact edits:

`Disable` — after the `PersistEnabled` block:
```go
	if m.host.PersistEnabled != nil {
		m.host.PersistEnabled(string(id), false)
	}
	m.notify()
}
```

`Enable` — after the `PersistEnabled` block:
```go
	if m.host.PersistEnabled != nil {
		m.host.PersistEnabled(string(id), true)
	}
	m.notify()
}
```

`disableScript` — after the warn log:
```go
func (m *Manager) disableScript(id extension.ID, reason string) {
	m.reg.DisableAsRunaway(id, reason)
	logger.Log.Warn().Str("script", string(id)).Str("reason", reason).Msg("script auto-disabled by watchdog")
	m.notify()
}
```

`reload` — after `m.loadDir(dir)`:
```go
func (m *Manager) reload(dir string) {
	id := extension.ID(filepath.Base(dir))
	m.sched.stopTimers(id)
	m.loadDir(dir)
	m.notify()
}
```

`unload` — after `m.reg.Remove(id)`:
```go
	m.reg.Remove(id)
	m.notify()
}
```

`handleFSEvent` — in the new-directory create branch, after `m.loadDir(ev.Name)`:
```go
		if fi, err := os.Stat(ev.Name); err == nil && fi.IsDir() && filepath.Dir(ev.Name) == m.dir {
			_ = m.watcher.Add(ev.Name)
			m.loadDir(ev.Name)
			m.notify()
			return
		}
```

> Note: `loadDir` is intentionally NOT instrumented directly — it runs once per script during `LoadAll` startup, and instrumenting it would emit a burst before the frontend has mounted. The `reload`/`handleFSEvent` callers cover every *post-startup* load. The one startup emit that does occur is from `LoadAll`'s `m.Disable(id)` for persisted-disabled scripts; it is harmless (the store simply refetches).

- [ ] **Step 5: Run test to verify it passes**

Run: `CGO_ENABLED=1 go test -tags fts5 -race ./internal/script/ -run TestManagerNotifiesOnLifecycleTransitions -v`
Expected: PASS.

- [ ] **Step 6: Wire `Notify` in `app.go`**

In the `script.Host{…}` literal, add after the `PersistEnabled` field:

```go
		PersistEnabled: func(id string, enabled bool) {
			_ = app.storage.SetScriptEnabled(id, enabled)
		},
		Notify: func() {
			app.emit("script-lifecycle", map[string]any{})
		},
	})
```

- [ ] **Step 7: Build the app + run the full backend suite**

Run:
```bash
mkdir -p frontend/dist && printf '<!doctype html>' > frontend/dist/index.html && go build .
CGO_ENABLED=1 go test -tags fts5 -race ./internal/script/ -count=1
CGO_ENABLED=1 go test -tags fts5 ./internal/... -count=1
gofmt -l internal/script/manager.go app.go
go vet ./...
```
Expected: build succeeds; tests PASS; `gofmt -l` prints nothing; `go vet` clean.

- [ ] **Step 8: Commit**

```bash
git add internal/script/manager.go internal/script/manager_test.go app.go
git commit -m "feat(script): emit script-lifecycle on status transitions (PR10 backend)"
```

---

## Task 2: Frontend — `stores/scripts.ts` Zustand store + `initScripts()`

**Files:**
- Create: `frontend/src/stores/scripts.ts`
- Test: `frontend/src/stores/scripts.test.ts`
- Modify: `frontend/src/main.tsx` (call `initScripts()`)

**Interfaces:**
- Consumes: `ListScripts`, `EnableScript`, `DisableScript`, `ReloadScript`, `NewScript`, `OpenScriptsDir` from `'../../wailsjs/go/main/App'`; `main.ScriptInfo` from `'../../wailsjs/go/models'`; `EventsOn` from `'../../wailsjs/runtime/runtime'`.
- Produces: `useScriptsStore` with state `{ scripts: main.ScriptInfo[]; loading: boolean; busy: Set<string>; lastCreatedPath: string | null; error: string | null }` and actions `fetch()`, `enable(id)`, `disable(id)`, `reload(id)`, `create(name)`, `openDir()`, `clearCreated()`; exported `initScripts(): void`.

- [ ] **Step 1: Write the failing test**

```ts
// frontend/src/stores/scripts.test.ts
import { describe, it, expect, vi, beforeEach } from 'vitest';

const listScripts = vi.fn();
const enableScript = vi.fn(() => Promise.resolve());
const disableScript = vi.fn(() => Promise.resolve());
const reloadScript = vi.fn(() => Promise.resolve());
const newScript = vi.fn((_name: string) => Promise.resolve('/scripts/foo/foo.go'));
const openScriptsDir = vi.fn(() => Promise.resolve());
let lifecycleCb: (() => void) | null = null;

vi.mock('../../wailsjs/go/main/App', () => ({
  ListScripts: () => listScripts(),
  EnableScript: (id: string) => enableScript(id),
  DisableScript: (id: string) => disableScript(id),
  ReloadScript: (id: string) => reloadScript(id),
  NewScript: (name: string) => newScript(name),
  OpenScriptsDir: () => openScriptsDir(),
}));
vi.mock('../../wailsjs/runtime/runtime', () => ({
  EventsOn: vi.fn((_name: string, cb: () => void) => {
    lifecycleCb = cb;
    return () => {};
  }),
}));

import { useScriptsStore, initScripts } from './scripts';

const row = (over: Record<string, unknown> = {}) => ({
  id: 'greeter', name: 'greeter', description: '', status: 'loaded',
  enabled: true, error: '', perms: [], ...over,
});

describe('scripts store', () => {
  beforeEach(() => {
    listScripts.mockReset().mockResolvedValue([row()]);
    enableScript.mockClear(); disableScript.mockClear(); reloadScript.mockClear();
    newScript.mockClear(); openScriptsDir.mockClear();
    lifecycleCb = null;
    useScriptsStore.setState({ scripts: [], loading: false, busy: new Set(), lastCreatedPath: null, error: null });
  });

  it('fetch() populates scripts from ListScripts', async () => {
    await useScriptsStore.getState().fetch();
    expect(listScripts).toHaveBeenCalled();
    expect(useScriptsStore.getState().scripts).toHaveLength(1);
    expect(useScriptsStore.getState().scripts[0].id).toBe('greeter');
  });

  it('disable() calls DisableScript then refetches', async () => {
    await useScriptsStore.getState().disable('greeter');
    expect(disableScript).toHaveBeenCalledWith('greeter');
    expect(listScripts).toHaveBeenCalled();
  });

  it('enable() calls EnableScript then refetches', async () => {
    await useScriptsStore.getState().enable('greeter');
    expect(enableScript).toHaveBeenCalledWith('greeter');
    expect(listScripts).toHaveBeenCalled();
  });

  it('reload() calls ReloadScript then refetches', async () => {
    await useScriptsStore.getState().reload('greeter');
    expect(reloadScript).toHaveBeenCalledWith('greeter');
    expect(listScripts).toHaveBeenCalled();
  });

  it('create() stores the returned path and refetches', async () => {
    await useScriptsStore.getState().create('foo');
    expect(newScript).toHaveBeenCalledWith('foo');
    expect(useScriptsStore.getState().lastCreatedPath).toBe('/scripts/foo/foo.go');
  });

  it('create() surfaces backend errors and does not set a path', async () => {
    newScript.mockRejectedValueOnce(new Error('script "foo" already exists'));
    await useScriptsStore.getState().create('foo');
    expect(useScriptsStore.getState().error).toContain('already exists');
    expect(useScriptsStore.getState().lastCreatedPath).toBeNull();
  });

  it('initScripts() subscribes to script-lifecycle and refetches on it', async () => {
    initScripts();
    expect(lifecycleCb).toBeTypeOf('function');
    listScripts.mockClear();
    lifecycleCb!();
    await Promise.resolve();
    expect(listScripts).toHaveBeenCalled();
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && npx vitest run src/stores/scripts.test.ts`
Expected: FAIL — cannot resolve `./scripts` (module not created yet).

- [ ] **Step 3: Write the store**

```ts
// frontend/src/stores/scripts.ts
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd frontend && npx vitest run src/stores/scripts.test.ts`
Expected: PASS (all 7 cases). Rerun once if the flaky runner reports an unrelated failure.

- [ ] **Step 5: Wire `initScripts()` into `main.tsx`**

Add the import next to the other store inits (near line 8) and the call next to `initSettings()` (near line 76):

```ts
import { initScripts } from './stores/scripts'
```
```ts
void initSettings()
void initPreferences()
void initScripts()
```

- [ ] **Step 6: Commit**

```bash
git add frontend/src/stores/scripts.ts frontend/src/stores/scripts.test.ts frontend/src/main.tsx
git commit -m "feat(frontend): scripts Zustand store + script-lifecycle subscription"
```

---

## Task 3: Frontend — `components/scripts-panel.tsx`

**Files:**
- Create: `frontend/src/components/scripts-panel.tsx`
- Test: `frontend/src/components/__tests__/scripts-panel.test.tsx`

**Interfaces:**
- Consumes: `useScriptsStore` (Task 2).
- Produces: `export function ScriptsPanel(): JSX.Element` — fetches on mount; renders a header (New-script input + create, "Open scripts folder", and the just-created path with a "Reveal in folder" button) and one row per script (name, description, status badge, perms, error, enable/disable toggle, reload). No props.

- [ ] **Step 1: Write the failing test**

```tsx
// frontend/src/components/__tests__/scripts-panel.test.tsx
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';

const fetch = vi.fn(() => Promise.resolve());
const enable = vi.fn(() => Promise.resolve());
const disable = vi.fn(() => Promise.resolve());
const reload = vi.fn(() => Promise.resolve());
const create = vi.fn(() => Promise.resolve());
const openDir = vi.fn(() => Promise.resolve());
let state: Record<string, unknown>;

vi.mock('../../stores/scripts', () => ({
  useScriptsStore: (sel: (s: Record<string, unknown>) => unknown) =>
    sel({ ...state, fetch, enable, disable, reload, create, openDir, clearCreated: vi.fn() }),
}));

import { ScriptsPanel } from '../scripts-panel';

const row = (over: Record<string, unknown> = {}) => ({
  id: 'greeter', name: 'greeter', description: 'says hi', status: 'loaded',
  enabled: true, error: '', perms: [], ...over,
});

describe('ScriptsPanel', () => {
  beforeEach(() => {
    fetch.mockClear(); enable.mockClear(); disable.mockClear();
    reload.mockClear(); create.mockClear(); openDir.mockClear();
    state = { scripts: [], loading: false, busy: new Set(), lastCreatedPath: null, error: null };
  });

  it('fetches on mount', () => {
    render(<ScriptsPanel />);
    expect(fetch).toHaveBeenCalled();
  });

  it('renders an empty state when there are no scripts', () => {
    render(<ScriptsPanel />);
    expect(screen.getByText(/no scripts/i)).toBeInTheDocument();
  });

  it('renders a script row with its name and status', () => {
    state.scripts = [row()];
    render(<ScriptsPanel />);
    expect(screen.getByText('greeter')).toBeInTheDocument();
    expect(screen.getByText(/loaded/i)).toBeInTheDocument();
  });

  it('shows the error text and a Runaway badge for a runaway script', () => {
    state.scripts = [row({ status: 'runaway', enabled: false, error: 'dispatch exceeded 1s deadline' })];
    render(<ScriptsPanel />);
    expect(screen.getByText(/runaway/i)).toBeInTheDocument();
    expect(screen.getByText(/dispatch exceeded 1s deadline/i)).toBeInTheDocument();
  });

  it('disables an enabled script when its toggle is clicked', () => {
    state.scripts = [row({ enabled: true })];
    render(<ScriptsPanel />);
    fireEvent.click(screen.getByRole('button', { name: /disable/i }));
    expect(disable).toHaveBeenCalledWith('greeter');
  });

  it('enables a disabled script when its toggle is clicked', () => {
    state.scripts = [row({ enabled: false, status: 'disabled' })];
    render(<ScriptsPanel />);
    fireEvent.click(screen.getByRole('button', { name: /enable/i }));
    expect(enable).toHaveBeenCalledWith('greeter');
  });

  it('reloads a script when its Reload button is clicked', () => {
    state.scripts = [row()];
    render(<ScriptsPanel />);
    fireEvent.click(screen.getByRole('button', { name: /reload/i }));
    expect(reload).toHaveBeenCalledWith('greeter');
  });

  it('creates a script from the name input', () => {
    render(<ScriptsPanel />);
    fireEvent.change(screen.getByPlaceholderText(/script name/i), { target: { value: 'mybot' } });
    fireEvent.click(screen.getByRole('button', { name: /^new script$/i }));
    expect(create).toHaveBeenCalledWith('mybot');
  });

  it('shows the created path with a Reveal button after creation', () => {
    state.lastCreatedPath = '/scripts/mybot/mybot.go';
    render(<ScriptsPanel />);
    expect(screen.getByText('/scripts/mybot/mybot.go')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: /reveal in folder/i }));
    expect(openDir).toHaveBeenCalled();
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && npx vitest run src/components/__tests__/scripts-panel.test.tsx`
Expected: FAIL — cannot resolve `../scripts-panel`.

- [ ] **Step 3: Write the component**

```tsx
// frontend/src/components/scripts-panel.tsx
import { useEffect, useState } from 'react';
import { useScriptsStore } from '../stores/scripts';
import { main } from '../../wailsjs/go/models';

// statusBadge maps an extension status string to a label + Tailwind classes.
// Status values come from internal/extension: loaded | disabled | error | runaway.
function statusBadge(status: string): { label: string; className: string } {
  switch (status) {
    case 'loaded':
      return { label: 'Loaded', className: 'bg-green-500/15 text-green-600 dark:text-green-400' };
    case 'disabled':
      return { label: 'Disabled', className: 'bg-muted text-muted-foreground' };
    case 'runaway':
      return { label: 'Runaway', className: 'bg-destructive/15 text-destructive' };
    case 'error':
      return { label: 'Error', className: 'bg-destructive/15 text-destructive' };
    default:
      return { label: status || 'Unknown', className: 'bg-muted text-muted-foreground' };
  }
}

function ScriptRow({ script }: { script: main.ScriptInfo }) {
  const busy = useScriptsStore((s) => s.busy.has(script.id));
  const enable = useScriptsStore((s) => s.enable);
  const disable = useScriptsStore((s) => s.disable);
  const reload = useScriptsStore((s) => s.reload);
  const badge = statusBadge(script.status);

  return (
    <div className="border border-border rounded p-4">
      <div className="flex items-center justify-between mb-2">
        <div className="flex items-center gap-2">
          <h4 className="font-semibold">{script.name}</h4>
          <span className={`px-2 py-0.5 text-xs rounded ${badge.className}`}>{badge.label}</span>
        </div>
        <div className="flex items-center gap-2">
          <button
            onClick={() => reload(script.id)}
            disabled={busy}
            className="px-3 py-1.5 text-xs border border-border rounded-lg hover:bg-accent transition-all disabled:opacity-50 disabled:cursor-not-allowed"
          >
            Reload
          </button>
          <button
            onClick={() => (script.enabled ? disable(script.id) : enable(script.id))}
            disabled={busy}
            className="px-3 py-1.5 text-xs border border-border rounded-lg hover:bg-accent transition-all disabled:opacity-50 disabled:cursor-not-allowed"
          >
            {script.enabled ? 'Disable' : 'Enable'}
          </button>
        </div>
      </div>
      {script.description && (
        <p className="text-sm text-muted-foreground mb-2">{script.description}</p>
      )}
      {script.perms && script.perms.length > 0 && (
        <div className="flex flex-wrap gap-1 mb-2">
          {script.perms.map((p) => (
            <span key={p} className="px-2 py-0.5 text-xs rounded bg-muted text-muted-foreground">{p}</span>
          ))}
        </div>
      )}
      {script.error && (
        <p className="text-sm text-destructive break-words">{script.error}</p>
      )}
    </div>
  );
}

export function ScriptsPanel() {
  const scripts = useScriptsStore((s) => s.scripts);
  const error = useScriptsStore((s) => s.error);
  const lastCreatedPath = useScriptsStore((s) => s.lastCreatedPath);
  const fetch = useScriptsStore((s) => s.fetch);
  const create = useScriptsStore((s) => s.create);
  const openDir = useScriptsStore((s) => s.openDir);
  const clearCreated = useScriptsStore((s) => s.clearCreated);
  const [name, setName] = useState('');

  useEffect(() => {
    void fetch();
  }, [fetch]);

  const onCreate = () => {
    const trimmed = name.trim();
    if (!trimmed) return;
    void create(trimmed);
    setName('');
  };

  return (
    <div className="mb-6">
      <div className="flex items-center justify-between mb-4">
        <h3 className="text-md font-semibold">Scripts</h3>
        <button
          onClick={() => openDir()}
          className="px-3 py-1.5 text-xs border border-border rounded-lg hover:bg-accent transition-all"
        >
          Open scripts folder
        </button>
      </div>

      {/* New script */}
      <div className="flex items-center gap-2 mb-3">
        <input
          value={name}
          onChange={(e) => setName(e.target.value)}
          onKeyDown={(e) => { if (e.key === 'Enter') onCreate(); }}
          placeholder="script name"
          className="flex-1 px-3 py-1.5 text-sm border border-border rounded-lg bg-background"
        />
        <button
          onClick={onCreate}
          className="px-3 py-1.5 text-xs border border-border rounded-lg hover:bg-accent transition-all"
        >
          New script
        </button>
      </div>

      {lastCreatedPath && (
        <div className="flex items-center justify-between gap-2 mb-3 p-2 border border-border rounded bg-muted/30">
          <code className="text-xs break-all">{lastCreatedPath}</code>
          <div className="flex items-center gap-2 flex-shrink-0">
            <button
              onClick={() => openDir()}
              className="px-3 py-1.5 text-xs border border-border rounded-lg hover:bg-accent transition-all"
            >
              Reveal in folder
            </button>
            <button
              onClick={() => clearCreated()}
              className="px-2 py-1.5 text-xs text-muted-foreground hover:text-foreground"
            >
              Dismiss
            </button>
          </div>
        </div>
      )}

      {error && <p className="text-sm text-destructive mb-3">{error}</p>}

      {scripts.length === 0 ? (
        <div className="text-center text-muted-foreground py-8">
          No scripts yet. Create one above, then edit its <code>.go</code> file.
        </div>
      ) : (
        <div className="space-y-3">
          {scripts.map((s) => (
            <ScriptRow key={s.id} script={s} />
          ))}
        </div>
      )}
    </div>
  );
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd frontend && npx vitest run src/components/__tests__/scripts-panel.test.tsx`
Expected: PASS (all cases). Rerun once on a spurious flaky failure.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/scripts-panel.tsx frontend/src/components/__tests__/scripts-panel.test.tsx
git commit -m "feat(frontend): scripts-panel component (inventory, toggle, reload, new script)"
```

---

## Task 4: Frontend — wire the Scripts section into the settings panel

**Files:**
- Modify: `frontend/src/components/settings-panel.tsx` (type + guard line 17–20; `renderContent` switch ~line 1070; `navItems` ~line 1518)
- Test: `frontend/src/components/__tests__/settings-panel.test.tsx`

**Interfaces:**
- Consumes: `ScriptsPanel` (Task 3).
- Produces: `'scripts'` added to `SettingsSection` and `isSettingsSection`; a `{ id: 'scripts', label: 'Scripts' }` nav item; a `case 'scripts'` that renders `<ScriptsPanel/>`.

- [ ] **Step 1: Write the failing test**

Add a case to `frontend/src/components/__tests__/settings-panel.test.tsx` (follow the file's existing render/mock setup; if it renders `<SettingsPanel section="…" onSectionChange={…} />`, mirror that). The new assertion:

```tsx
it('renders the Scripts panel when the scripts section is active', () => {
  render(<SettingsPanel section="scripts" onSectionChange={() => {}} />);
  // ScriptsPanel renders its "Open scripts folder" header button.
  expect(screen.getByRole('button', { name: /open scripts folder/i })).toBeInTheDocument();
});
```

> If `scripts-panel`'s store import causes the bound Wails modules to load in this test, add `vi.mock('../scripts-panel', () => ({ ScriptsPanel: () => <button>Open scripts folder</button> }))` at the top of the test file — the goal here is to prove the *wiring* (section → panel), not re-test the panel.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && npx vitest run src/components/__tests__/settings-panel.test.tsx`
Expected: FAIL — the `scripts` section renders nothing (no matching `case`), so the button is absent.

- [ ] **Step 3: Add the type member + guard**

Line 17:
```ts
export type SettingsSection = 'networks' | 'plugins' | 'scripts' | 'display' | 'notifications' | 'advanced' | 'about';
```
Lines 19–20:
```ts
export const isSettingsSection = (v: string): v is SettingsSection =>
  v === 'networks' || v === 'plugins' || v === 'scripts' || v === 'display' || v === 'notifications' || v === 'advanced' || v === 'about';
```

- [ ] **Step 4: Import `ScriptsPanel` and add the render case + nav item**

Add the import near the top with the other component imports:
```ts
import { ScriptsPanel } from './scripts-panel';
```

In `renderContent()`'s `switch`, add a case (place it right after the `case 'plugins':` block returns):
```tsx
      case 'scripts':
        return <ScriptsPanel />;
```

In the `navItems` array, add the Scripts item after Plugins:
```ts
    { id: 'networks', label: 'Networks' },
    { id: 'plugins', label: 'Plugins' },
    { id: 'scripts', label: 'Scripts' },
    { id: 'display', label: 'Display' },
    { id: 'notifications', label: 'Notifications' },
    { id: 'advanced', label: 'Advanced' },
    { id: 'about', label: 'About' },
```

- [ ] **Step 5: Run test to verify it passes**

Run: `cd frontend && npx vitest run src/components/__tests__/settings-panel.test.tsx`
Expected: PASS.

- [ ] **Step 6: Run the full frontend suite + type-check**

Run:
```bash
cd frontend && npx vitest run && npx tsc --noEmit
```
Expected: all tests PASS (rerun once on flaky failure); `tsc` reports no errors.

- [ ] **Step 7: Commit**

```bash
git add frontend/src/components/settings-panel.tsx frontend/src/components/__tests__/settings-panel.test.tsx
git commit -m "feat(frontend): add Scripts section to the settings panel nav"
```

---

## PR10 Self-Review

- **Spec coverage:** inventory/status ✓ (T2 store + T3 rows), enable/disable toggle ✓ (T3), reload ✓ (T3), "New script" + reveal ✓ (T3, Decision 3), error display ✓ (T3), nav integration ✓ (T4), live status incl. watchdog runaway ✓ (T1 emit + T2 subscription, Decision 2), extracted component + Zustand store ✓ (Decision 1).
- **No backend API surface change:** only an additive `Host.Notify` + a runtime event; PR9's bound methods and `ScriptInfo` model are reused unchanged, so `wails3 generate bindings` is intentionally not run.
- **Type consistency:** store actions `fetch/enable/disable/reload/create/openDir/clearCreated`, state `scripts/loading/busy/lastCreatedPath/error`, and `main.ScriptInfo` fields `id/name/description/status/enabled/error/perms` are used identically across Tasks 2–4.
- **Deferred (unchanged scope):** permission *enforcement* + quarantine/trust UI, `parseManifest` hardening, configurable watchdog/timer limits → v2. Public docs → PR11.

## After PR10 — remaining work

| Item | Where |
|------|-------|
| Public docs (`docs/public/developers/`) for the scripting system | **PR11** |
| Deferred actions: `Network.Notice`/bot `Join`/`Part`, `OnAction`, dynamic registration | **follow-up** |
| Permission enforcement + quarantine/trust UI; harden `parseManifest`; configurable watchdog/timer limits; `ResolveNetwork` name→id cache | **v2** |
| Cut the `cascade/v1.0.0` tag | **Release** |
