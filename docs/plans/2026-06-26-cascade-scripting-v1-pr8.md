# Cascade Scripting v1 — PR8: Proactive scripting (Setup · networks · timers)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Give scripts a *proactive* side. Build the `Setup(c)` entry point (the second half of the hybrid event model), hand the script a `cascade.Client`, and through it let scripts **send to any channel/network** (`c.Network(name).Say(...)`) and **run periodic logic** (`c.Every`/`c.After`). All proactive work runs through the same per-script-serialized, watchdog-protected dispatch as events.

**Architecture:** After a script loads, the Manager calls its exported `Setup(*cascade.Client)` (if present), passing a `Client` whose unexported callbacks are bound to the Manager. `c.Network(name)` returns a `Network` handle; `Network.Say(target,msg)` resolves the name→networkID (via an App resolver) and sends through `App.SendMessage` (fire-and-forget, logged on error). `c.Every/c.After` register per-script timers in a scheduler; each tick runs the script's func through `watchdog.run` + the per-script mutex (so a hanging/panicking timer disables the script and never races event dispatch). Timers are cancelled on unload/reload (then `Setup` re-runs, re-registering them).

**Tech Stack:** Go; the PR4–7 manager + watchdog; the cascade SDK module.

## Global Constraints

- **Module path lowercase** `github.com/matt0x6f/irc-client`; cascade SDK `github.com/matt0x6f/irc-client/cascade`.
- **Tests:** `CGO_ENABLED=1 go test -tags fts5 <pkg> -count=1`; `internal/script` concurrency-bearing → `-race`. cascade module from its dir. gofmt/vet clean.
- **All script code runs guarded:** `Setup` is panic-recovered (a panicking Setup → errored script, not a crash); every timer tick runs through `watchdog.run(id, ...)` + the per-script `l.mu` (same protection as event dispatch).
- **Timer lifecycle is load-bearing:** reload/unload MUST cancel a script's timers before re-running Setup, or timers leak/duplicate. Track per-script cancellation.
- **cascade pre-release** → API additions are fine; finalize before the `v1.0.0` tag.
- **App `package main` needs a stub `frontend/dist`** to build (Task 4 wiring): `mkdir -p frontend/dist && printf '<!doctype html>' > frontend/dist/index.html` (git-ignored; never commit).

## Scope (explicit)

**In:** `Setup(c)`, `c.Network(name).Say(target,message)`, `c.Every(interval, fn)`, `c.After(delay, fn)`.
**Deferred (separable; see After PR8):** `Network.Notice`/`Join`/`Part` (need new outbound IRC send paths), `OnAction` (producer decision), dynamic `c.OnText(filter, fn)` registration.

## Grounded facts

- `App.GetNetworks() ([]storage.Network, error)` — each `storage.Network` has `ID int64` + `Name string`. The name→id resolver iterates this.
- `App.SendMessage(networkID int64, target, message string) error` — the send path (already the event `Sender`).
- The Manager already has: `watchdog.run(id, func() error)`, the per-script `*loaded{ script, mu }` map, `loadDir`/`reload`/`unload`, `disableScript`.
- Interval strings parse with `time.ParseDuration` (e.g. `"5m"`, `"30s"`).

---

## PR8 — File Structure

- `cascade/client.go` + `client_test.go` — `Client` + `Network` types (Task 1).
- `internal/script/syms/` — regenerated symbol table (Task 1).
- `internal/script/engine.go` — `Script.HasSetup()` + `Script.RunSetup(c)` (Task 2).
- `internal/script/manager.go` — host-capabilities struct, `Client` construction, `Setup` call in `loadDir`, scheduler + timer lifecycle (Tasks 3–4).
- `internal/script/scheduler.go` + tests — the per-script timer scheduler (Task 4).
- `app.go` — provide the network resolver to `NewManager` (Task 3).

---

## Task 1: cascade SDK — `Client` + `Network`

**Files:** Modify `cascade/client.go` (new), `cascade/client_test.go` (new); regenerate symbols.

**Interfaces (host-constructed; scripts use):**
- `type Client struct{ ... }`; `func (c *Client) Network(name string) Network`; `func (c *Client) Every(interval string, fn func())`; `func (c *Client) After(delay string, fn func())`.
- `type Network struct{ ... }`; `func (n Network) Say(target, message string)`.
- Host constructor: `func NewClient(say func(networkName, target, message string), every func(interval string, fn func()), after func(delay string, fn func())) *Client`.

- [ ] **Step 1: Failing test (cascade module)**

```go
// cascade/client_test.go
package cascade

import (
	"sync"
	"testing"
)

func TestClientNetworkSayAndTimers(t *testing.T) {
	var mu sync.Mutex
	var sends [][3]string
	var everies, afters []string

	c := NewClient(
		func(net, target, msg string) { mu.Lock(); sends = append(sends, [3]string{net, target, msg}); mu.Unlock() },
		func(interval string, fn func()) { everies = append(everies, interval); fn() },
		func(delay string, fn func()) { afters = append(afters, delay); fn() },
	)

	c.Network("libera").Say("#go", "hi")
	c.Every("5m", func() { c.Network("libera").Say("#go", "tick") })
	c.After("30s", func() {})

	mu.Lock()
	defer mu.Unlock()
	if len(sends) != 2 || sends[0] != [3]string{"libera", "#go", "hi"} || sends[1] != [3]string{"libera", "#go", "tick"} {
		t.Fatalf("sends = %v", sends)
	}
	if len(everies) != 1 || everies[0] != "5m" || len(afters) != 1 || afters[0] != "30s" {
		t.Fatalf("timers = %v %v", everies, afters)
	}
}
```

- [ ] **Step 2: Run — fails (NewClient undefined).** `(cd cascade && CGO_ENABLED=1 go test ./... -run TestClientNetworkSayAndTimers -v)`

- [ ] **Step 3: Implement `cascade/client.go`**

```go
package cascade

// Client is the script's proactive handle, passed to Setup(c). Its callbacks are
// bound by the host; scripts never construct it.
type Client struct {
	sayFn   func(networkName, target, message string)
	everyFn func(interval string, fn func())
	afterFn func(delay string, fn func())
}

// NewClient is the host-side constructor.
func NewClient(say func(networkName, target, message string), every func(interval string, fn func()), after func(delay string, fn func())) *Client {
	return &Client{sayFn: say, everyFn: every, afterFn: after}
}

// Network returns a handle to a configured network by name. Actions resolve the
// network at call time, so a handle to a disconnected/unknown network simply
// no-ops (the host logs it).
func (c *Client) Network(name string) Network { return Network{name: name, sayFn: c.sayFn} }

// Every schedules fn to run repeatedly on the given interval (e.g. "5m", "30s").
func (c *Client) Every(interval string, fn func()) {
	if c.everyFn != nil {
		c.everyFn(interval, fn)
	}
}

// After schedules fn to run once after the given delay.
func (c *Client) After(delay string, fn func()) {
	if c.afterFn != nil {
		c.afterFn(delay, fn)
	}
}

// Network is a handle to one configured network.
type Network struct {
	name  string
	sayFn func(networkName, target, message string)
}

// Say sends a PRIVMSG to target on this network. Fire-and-forget.
func (n Network) Say(target, message string) {
	if n.sayFn != nil {
		n.sayFn(n.name, target, message)
	}
}
```

- [ ] **Step 4: Regenerate symbols + green + commit**

Run `task script-symbols` (it must pick up `Client`, `Network`, `NewClient`). Then `(cd cascade && CGO_ENABLED=1 go test ./... -count=1)` and a script-suite run to confirm the symbol table is valid.
```bash
git add cascade/ internal/script/syms/
git commit -m "feat(cascade): add Client + Network (proactive send + timers)"
```

---

## Task 2: Engine — call `Setup(c)`

**Files:** Modify `internal/script/engine.go` (+ a test).

**Interfaces:**
- Produces: `func (s *Script) HasSetup() bool` (i.e. `Has("Setup")`); `func (s *Script) RunSetup(c *cascade.Client) (err error)` — reflect-calls `main.Setup(c)`, recovering panics into `err`. No-op (nil) if absent.

- [ ] **Step 1: Failing test** — a fixture-dir loaded via `LoadPackage` whose `Setup(c)` calls `c.After("0s", ...)`; assert `RunSetup` invokes it. (Construct the `*cascade.Client` with spy callbacks; assert the spy saw the `After`.)

- [ ] **Step 2/3: Implement** — mirror `DispatchText`'s reflect-call, but pass `reflect.ValueOf(c)` (a `*cascade.Client`), and wrap in `defer/recover` returning the panic as an error. `HasSetup()` = `s.Has("Setup")`.

- [ ] **Step 4: Green + commit** (`feat(script): invoke script Setup(c) with panic recovery`).

---

## Task 3: Manager — Client construction, `Setup` call, network send wiring

**Files:** Modify `internal/script/manager.go`, `internal/script/manager_test.go`, `app.go`.

**Interfaces:**
- The proliferating constructor params are collapsed into a capabilities struct:
  ```go
  type Host struct {
      Send           Sender                         // App.SendMessage
      SelfNick       func(networkID int64) string   // App.GetCurrentNick wrapper
      ResolveNetwork func(name string) (int64, bool) // name -> networkID (via App.GetNetworks)
  }
  func NewManager(bus *events.EventBus, dir string, host Host) *Manager
  ```
  Refactor every existing `NewManager(bus, dir, send, selfNick)` caller (tests + app.go) to pass a `Host{...}`. Behavior unchanged for events (Reply still uses `host.Send`).
- The Manager builds a per-script `*cascade.Client` whose `sayFn` resolves the network name → id and calls `host.Send`; a network that doesn't resolve logs a Warn and drops:
  ```go
  func (m *Manager) makeClient(id extension.ID) *cascade.Client {
      say := func(networkName, target, message string) {
          netID, ok := m.host.ResolveNetwork(networkName)
          if !ok {
              logger.Log.Warn().Str("script", string(id)).Str("network", networkName).Msg("script Say: unknown network")
              return
          }
          _ = m.host.Send(netID, target, message)
      }
      return cascade.NewClient(say, m.scheduleEvery(id), m.scheduleAfter(id)) // schedulers from Task 4
  }
  ```
  (In THIS task, stub `scheduleEvery`/`scheduleAfter` to no-op closures so the proactive-send half compiles and tests; Task 4 implements real scheduling.)
- In `loadDir`, after a successful load, if `s.HasSetup()` run it under recovery and mark the script errored on failure:
  ```go
  if s.HasSetup() {
      if err := s.RunSetup(m.makeClient(id)); err != nil {
          ext.Status = extension.StatusError; ext.Err = "Setup: " + err.Error()
          // keep it registered+errored; do not subscribe a script whose Setup failed
          ...
      }
  }
  ```
- `app.go`: build the `Host`:
  ```go
  host := script.Host{
      Send:     app.SendMessage,
      SelfNick: func(id int64) string { n, _ := app.GetCurrentNick(id); return n },
      ResolveNetwork: func(name string) (int64, bool) {
          nets, err := app.GetNetworks()
          if err != nil { return 0, false }
          for _, n := range nets { if n.Name == name { return n.ID, true } }
          return 0, false
      },
  }
  app.scriptMgr = script.NewManager(eventBus, scriptDir, host)
  ```

- [ ] **Steps:** failing test (a Setup-using fixture that `c.Network("netA").Say("#x","hello")` → assert the fake host's send got `(idA,"#x","hello")` after resolving "netA"); refactor `NewManager`+callers to `Host`; implement `makeClient` + the `loadDir` Setup call (scheduler stubbed); app wiring; verify `go build .` (stub dist) + `-race` + full suite; commit (`feat(script): Setup(c) + c.Network().Say wired to the App`).

---

## Task 4: Timer scheduler + lifecycle

**Files:** Create `internal/script/scheduler.go` + `scheduler_test.go`; modify `manager.go` (wire real `scheduleEvery`/`scheduleAfter`; cancel on `reload`/`unload`).

**Interfaces:**
- Produces: a per-script timer registry. `m.scheduleEvery(id) func(interval string, fn func())` / `m.scheduleAfter(id) func(delay string, fn func())` return the closures the Client calls. Each parses the duration; an invalid duration logs + no-ops. A registered timer runs its tick through `m.watchdog.run(id, func() error { return m.runScriptFn(id, fn) })`, where `runScriptFn` takes the per-script `l.mu` + recovers (mirrors `dispatch`). On `reload(id)`/`unload(id)`, all of a script's timers are stopped BEFORE Setup re-runs.

- [ ] **Step 1: Failing test (deterministic — NO real wall-clock waits)**

Design the scheduler so the tick mechanism is injectable, OR test with a very short interval + a `time.Sleep` poll with generous margin — BUT prefer determinism: expose an internal `fireNow(id)` test hook, or have `scheduleEvery` accept a tick source. Simplest deterministic test: `c.After("1ms", fn)` then poll up to ~1s for fn's effect (a sent message via the fake host), asserting it fired exactly once; and after `unload(id)`, assert no further fires. Keep margins generous; do not assert tight timing. Document that timing tests use a poll-with-timeout, not a fixed sleep.

- [ ] **Step 2/3: Implement the scheduler** — a `map[extension.ID][]*time.Timer` (or context-cancel funcs) guarded by a mutex. `Every` uses a self-rescheduling `time.AfterFunc` (or a ticker goroutine) whose callback runs the watchdog-wrapped tick and re-arms; `After` a one-shot `time.AfterFunc`. `stopTimers(id)` stops/cancels all and clears the entry. Call `stopTimers(id)` at the top of `reload` and in `unload`. `runScriptFn(id, fn)` looks up `*loaded`, locks `l.mu`, `defer recover`, calls `fn()`.

- [ ] **Step 4: Wire + verify** — replace the Task-3 stub schedulers with the real ones in `makeClient`; ensure `reload`/`unload` call `stopTimers`. `-race` (the scheduler + dispatch + watcher goroutines must be race-clean); full suite; gofmt. Commit (`feat(script): per-script timer scheduler (Every/After) with reload-safe lifecycle`).

---

## PR8 Self-Review

- **Spec coverage:** `Setup(c)` invoked + recovered ✓ (T2/T3); `c.Network(name).Say` resolves name→id + sends ✓ (T3); `c.Every`/`c.After` schedule, run watchdog-wrapped + per-script-serialized ✓ (T4); timers cancelled on reload/unload ✓ (T4).
- **Safety:** Setup panic → errored script (no crash); timer ticks share the watchdog + per-script mutex (a runaway timer auto-disables the script; no race with event dispatch).
- **No param sprawl:** `NewManager` takes a `Host` capabilities struct.
- **Deferred is explicit:** Notice/Join/Part/OnAction/dynamic-registration below.

## After PR8 — remaining deferred work

| Deferred | Where |
|----------|-------|
| `Network.Notice` / bot `Join` / `Part` (need new outbound IRC send paths) + `OnAction` (route the early-returning CTCP ACTION branch) | **PR8b / PR9-ish** |
| Dynamic `c.OnText(filter, fn)` registration (filters, multiple handlers per event) | later / v2 |
| **Frontend management & permissions surface** + enable/disable persistence + watchdog re-enable | **PR9 (frontend)** |
| **Docs site** | **PR10** |
| Permission enforcement; harden `parseManifest`; configurable watchdog/timer limits | **v2** |
| Cut the `cascade/v1.0.0` tag | **Release** |
