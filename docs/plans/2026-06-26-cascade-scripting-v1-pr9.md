# Cascade Scripting v1 — PR9: Backend management API (persistence · lifecycle · Wails)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Expose a complete, persisted management API for scripts that the frontend (PR10) will consume: list scripts with status, enable/disable (persisted across restarts, correctly stopping/restoring timers and clearing runaway state), reload, and scaffold a new script. No UI in this PR — Go only, fully unit-testable.

**Architecture:** A `script_state` SQLite table persists each script's enabled flag. The `Manager` gains `Enable`/`Disable`/`ReloadByID`/`NewScript`/`Snapshot` methods; `Enable`/`Disable` go through the persistence layer and fix the two deferred lifecycle gaps — **Disable stops the script's timers**, and **Enable resets `StatusRunaway`→`StatusLoaded`, clears the watchdog strike count, and re-runs `Setup`** (restoring timers). On startup, scripts in the persisted-disabled set load but are immediately disabled. Wails-bound `App` methods in `app_scripts.go` wrap the Manager; bindings are regenerated.

**Tech Stack:** Go; SQLite + SQLC (`internal/storage`); Wails v3 bindings.

## Global Constraints

- **Module path lowercase** `github.com/matt0x6f/irc-client`.
- **Tests:** `CGO_ENABLED=1 go test -tags fts5 ./internal/... -count=1`; `internal/script` also `-race`. gofmt/vet clean.
- **DB workflow:** edit `internal/storage/schema.sql` + add `internal/storage/queries/script_state.sql`, then run `task sqlc-generate` (regenerates `internal/storage/generated`). Mirror the existing `settings.sql` / `plugin_configs.sql` query style.
- **Wails:** bound methods live on the `App` struct in `package main`; **no `time.Time` across the boundary — ISO strings only** (none needed here, but if you add a timestamp, format it). After adding bound methods, run `wails3 generate bindings -clean=true` and commit the regenerated `frontend/wailsjs/` + bindings.
- **App `package main` needs a stub `frontend/dist`** to build (Task 3): `mkdir -p frontend/dist && printf '<!doctype html>' > frontend/dist/index.html` (git-ignored; never commit).
- **Preserve PR4–8 behavior:** events, watchdog, timers, hot reload all unchanged except the explicit lifecycle fixes below.

## Grounded facts

- `internal/storage` pattern: `schema.sql` (e.g. the `settings`/`plugin_configs` tables) + `queries/<name>.sql` + `task sqlc-generate` → `internal/storage/generated`. The `storage` package exposes generated query methods.
- `extension.Registry.SetEnabled(id, true)` only clears `StatusDisabled`→`StatusLoaded` — it does NOT touch `StatusRunaway`. So a runaway re-enable needs an explicit status reset (the registry already has `DisableAsRunaway`; add a symmetric clear or use `SetStatus(id, StatusLoaded, "")`).
- The watchdog holds `strikes map[extension.ID]int` (unexported) — add a `clearStrikes(id)` method for re-enable.
- The Manager already has: `loadDir`/`reload(dir)`/`unload(id)`, `m.sched.stopTimers(id)`, `makeClient(id)`, `disableScript`, `s.RunSetup(c)`, and the per-id `dir` is `filepath.Join(m.dir, string(id))`.

---

## PR9 — File Structure

- `internal/storage/schema.sql` + `internal/storage/queries/script_state.sql` + regenerated `generated/` — persistence (Task 1).
- `internal/script/watchdog.go` — `clearStrikes(id)` (Task 2).
- `internal/script/manager.go` — `Host` gains persistence callbacks; `Enable`/`Disable`/`ReloadByID`/`NewScript`/`Snapshot`; startup applies persisted-disabled (Task 2).
- `internal/script/manager_test.go` — lifecycle tests (Task 2).
- `app_scripts.go` (new) — Wails-bound methods; `app.go` wires the persistence callbacks into `Host` (Task 3).
- regenerated `frontend/wailsjs/` bindings (Task 3).

---

## Task 1: `script_state` persistence

**Files:** `internal/storage/schema.sql`, `internal/storage/queries/script_state.sql`, regenerated `generated/`; a thin storage accessor + test.

**Interfaces:**
- Table `script_state(script_id TEXT PRIMARY KEY, enabled INTEGER NOT NULL DEFAULT 1)`.
- SQLC queries (mirror `settings.sql` naming): `UpsertScriptEnabled(script_id, enabled)`, `ListDisabledScripts() -> []string` (or `ListScriptState() -> [(id,enabled)]`).
- A storage method the App will call: `SetScriptEnabled(id string, enabled bool) error` and `DisabledScripts() (map[string]bool, error)` (or return the disabled set).

- [ ] **Steps:** add the table to `schema.sql`; add `queries/script_state.sql` (upsert + list) mirroring an existing query file; run `task sqlc-generate`; add the storage accessor methods (follow how `settings`/`plugin_configs` accessors are written); a storage test (upsert disabled → list shows it; upsert enabled → gone from disabled set). Commit schema + queries + generated + accessor.

---

## Task 2: Manager lifecycle methods + watchdog clearStrikes

**Files:** `internal/script/watchdog.go`, `internal/script/manager.go`, `internal/script/manager_test.go`.

**Interfaces:**
- `func (w *watchdog) clearStrikes(id extension.ID)` — `delete(w.strikes, id)` under `w.mu`.
- `Host` gains persistence callbacks:
  ```go
  type Host struct {
      Send           Sender
      SelfNick       func(networkID int64) string
      ResolveNetwork func(name string) (int64, bool)
      LoadDisabled   func() map[string]bool        // ids persisted-disabled (nil-safe)
      PersistEnabled func(id string, enabled bool)  // persist a toggle (nil-safe)
  }
  ```
- Manager methods:
  - `func (m *Manager) Disable(id extension.ID)` — `m.sched.stopTimers(id)`; `m.reg.SetEnabled(id, false)`; if `m.host.PersistEnabled != nil` persist `(id,false)`. (Script inert: no events via recipients, no timers.)
  - `func (m *Manager) Enable(id extension.ID)` — `m.reg.SetEnabled(id, true)`; `m.reg.SetStatus(id, StatusLoaded, "")` (clears runaway/error); `m.watchdog.clearStrikes(id)`; re-run `Setup` to restore timers: look up the `*loaded`, and if it `HasSetup()`, `s.RunSetup(m.makeClient(id))` (recover/log on error); persist `(id,true)`.
  - `func (m *Manager) ReloadByID(id extension.ID)` — `m.reload(filepath.Join(m.dir, string(id)))`.
  - `func (m *Manager) NewScript(name string) (string, error)` — sanitize `name` to a safe dir segment; create `<dir>/<name>/<name>.go` with a starter template (the `// cascade:` header + a stub `OnText`); return the path; error if it already exists.
  - `func (m *Manager) Snapshot() []extension.Extension` — `m.reg.List()` (for the App's ListScripts).
- **Startup:** in `LoadAll`, after loading each script, if `m.host.LoadDisabled()[string(id)]` is true, call `m.Disable(id)` (so persisted-disabled scripts start inert). Load the disabled set once at the top of `LoadAll`.

- [ ] **Steps:** TDD each method. Tests with a fake `Host` (in-memory persisted set + spies): Disable stops timers + persists + filters from recipients; Enable clears runaway status + strikes + re-runs Setup (timer restored) + persists; a persisted-disabled script is inert after `LoadAll`; ReloadByID reloads; NewScript writes a valid loadable script (load it + assert `OnText` present). `-race`. Commit.

---

## Task 3: Wails-bound `app_scripts.go` + wiring + bindings

**Files:** `app_scripts.go` (new), `app.go` (wire persistence callbacks into `Host`), regenerated `frontend/wailsjs/`.

**Interfaces (all on `*App`, `package main`):**
```go
type ScriptInfo struct {
    ID, Name, Description, Status string
    Enabled bool
    Error   string
    Perms   []string
}
func (a *App) ListScripts() []ScriptInfo              // from scriptMgr.Snapshot()
func (a *App) EnableScript(id string) error           // scriptMgr.Enable
func (a *App) DisableScript(id string) error          // scriptMgr.Disable
func (a *App) ReloadScript(id string) error           // scriptMgr.ReloadByID
func (a *App) NewScript(name string) (string, error)  // scriptMgr.NewScript
func (a *App) OpenScriptsDir() error                  // reveal the scripts dir in the file manager
```
- `app.go`: extend the `script.Host` literal with `LoadDisabled: func() map[string]bool { d, _ := app.storage.DisabledScripts(); return d }` and `PersistEnabled: func(id string, enabled bool) { _ = app.storage.SetScriptEnabled(id, enabled) }`.
- `ListScripts` maps `extension.Extension` → `ScriptInfo` (Status as the string value).

- [ ] **Steps:** implement `app_scripts.go`; wire the two `Host` callbacks in `app.go`; `mkdir -p frontend/dist && printf '<!doctype html>' > frontend/dist/index.html && go build .`; run `wails3 generate bindings -clean=true` and confirm the regenerated `frontend/wailsjs/` includes the new methods + the `ScriptInfo` model (per the repo's bindings convention — committed bindings + the model renumber is benign); `CGO_ENABLED=1 go test -tags fts5 ./internal/... -count=1`; gofmt. Commit `app_scripts.go app.go` + the regenerated bindings (NOT frontend/dist).

> **Note:** `OpenScriptsDir` — reuse whatever "reveal in file manager" mechanism the app already uses (grep for an existing reveal/open-folder helper, or use the Wails runtime's browser-open on a `file://` path); if none exists, a minimal `exec`-based open per-GOOS is acceptable but flag it.

---

## PR9 Self-Review

- **Spec coverage:** persistence ✓ (T1); Disable stops timers + persists ✓, Enable resets runaway+strikes + re-runs Setup + persists ✓, ReloadByID/NewScript/Snapshot ✓, startup applies persisted-disabled ✓ (T2); Wails API + bindings ✓ (T3).
- **Deferred lifecycle gaps fixed here:** disable-stops-timers (PR8 note) and re-enable-resets-runaway+strikes (PR6/PR8 note) land with the methods that expose them.
- **No UI:** the React Scripts panel + store is PR10, building on this frozen API.

## After PR9 — remaining work

| Item | Where |
|------|-------|
| **Frontend Scripts panel** (React + ShadCN + Zustand store) consuming this API: inventory/status, enable/disable toggle, reload, "New script", error display, nav integration | **PR10 (frontend)** |
| Deferred actions: `Network.Notice`/bot `Join`/`Part`, `OnAction`, dynamic registration | **follow-up** |
| Public docs | **PR11** |
| Permission enforcement (perms become load-bearing) + quarantine/trust UI; harden `parseManifest`; configurable watchdog/timer limits; `ResolveNetwork` name→id cache | **v2** |
| Cut the `cascade/v1.0.0` tag | **Release** |
