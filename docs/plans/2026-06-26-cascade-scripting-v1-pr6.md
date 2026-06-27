# Cascade Scripting v1 — PR6: Resource watchdog

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Make a misbehaving script self-limiting. A dispatch that hangs (infinite loop) disables the script; a script that panics repeatedly is disabled after N strikes; and a script never reacts to its own (possibly echoed) messages, so it can't drive a reply loop. All three auto-disable the offender (`StatusRunaway`, `Enabled=false` → the Router stops delivering to it) — no Router changes.

**Architecture:** A `watchdog` runs each dispatch in a goroutine bounded by a deadline; on overrun it disables the script (Go cannot kill the goroutine, so it leaks but receives no further events). Panics (recovered) are counted as strikes; N consecutive strikes disable the script; a success clears the count. The `script.Manager` composes the watchdog around its existing per-script-serialized dispatch, and gains a `selfNick` lookup so `buildTextEvent` drops the bot's own messages.

**Tech Stack:** Go; the PR4/PR5 manager; the PR2 extension core.

## Global Constraints

- **Module path lowercase** `github.com/matt0x6f/irc-client`.
- **Tests:** `CGO_ENABLED=1 go test -tags fts5 <pkg> -count=1`; `internal/script` concurrency-bearing → also `-race`. gofmt-clean; `go vet`.
- **Honest limit (restate):** Go cannot preempt a goroutine, so a deadline overrun **disables** the script (stops future dispatch) but the runaway goroutine **leaks**. We contain blast radius; we do not kill. Document this in the watchdog's doc comment.
- **Do NOT run a real infinite-loop fixture in tests** — it would leak a CPU-spinning goroutine for the test process's life. The deadline path is unit-tested with a *controllable blocking* dispatch func (a channel you close to release the goroutine cleanly). The integration test exercises the **panic-strikes** path (deterministic, no leak).
- **Preserve PR4/PR5 safety:** per-script dispatch serialization, panic recovery (relocated, still present), reload/unload, hot reload — all unchanged in behavior.
- **App `package main` needs a stub `frontend/dist` to build** (Task 2 wiring): `mkdir -p frontend/dist && printf '<!doctype html>' > frontend/dist/index.html` (git-ignored; never commit).

## Grounded facts

- `App.GetCurrentNick(networkID int64) (string, error)` (`app_connection.go:961`) → the `selfNick` source (wrap to return `""` on error).
- `extension.Registry.SetEnabled(id, false)` sets `StatusDisabled`; follow with `SetStatus(id, StatusRunaway, reason)` so the *reason* shows. `recipients()` filters on `Enabled`, so a disabled script gets no events.
- The event's `Data["user"]` is the sender; for the bot's own echoed message it equals the bot's current nick.

---

## PR6 — File Structure

- `internal/script/watchdog.go` + `watchdog_test.go` — the watchdog core (Task 1).
- `internal/script/manager.go` — `selfNick` field + `buildTextEvent` self-skip (Task 2); watchdog composition + `dispatch`/`disableScript` (Task 3).
- `internal/script/manager_test.go` — self-skip + auto-disable tests (Tasks 2–3).
- `app.go` — pass a `selfNick` closure into `NewManager` (Task 2).

---

## Task 1: Watchdog core

**Files:**
- Create: `internal/script/watchdog.go`, `internal/script/watchdog_test.go`

**Interfaces:**
- Produces: `func newWatchdog(deadline time.Duration, maxStrikes int, disable func(extension.ID, string)) *watchdog`; `func (w *watchdog) run(id extension.ID, dispatch func() error) error`. Package consts `defaultDispatchDeadline`, `defaultMaxStrikes`.

- [ ] **Step 1: Write the failing test**

```go
// internal/script/watchdog_test.go
package script

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/matt0x6f/irc-client/internal/extension"
)

type disableSpy struct {
	mu     sync.Mutex
	calls  []extension.ID
}

func (d *disableSpy) fn(id extension.ID, _ string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls = append(d.calls, id)
}
func (d *disableSpy) count() int { d.mu.Lock(); defer d.mu.Unlock(); return len(d.calls) }

func TestWatchdogDeadlineDisables(t *testing.T) {
	spy := &disableSpy{}
	w := newWatchdog(20*time.Millisecond, 3, spy.fn)

	block := make(chan struct{})
	err := w.run("hang", func() error { <-block; return nil }) // blocks past the deadline
	if err == nil {
		t.Fatalf("expected a deadline error")
	}
	if spy.count() != 1 || spy.calls[0] != "hang" {
		t.Fatalf("expected disable('hang'); got %v", spy.calls)
	}
	close(block) // release the goroutine cleanly (no leak/CPU in the test)
}

func TestWatchdogDisablesAfterNStrikes(t *testing.T) {
	spy := &disableSpy{}
	w := newWatchdog(time.Second, 3, spy.fn)
	boom := func() error { return errors.New("boom") }

	w.run("p", boom)
	w.run("p", boom)
	if spy.count() != 0 {
		t.Fatalf("must not disable before maxStrikes; got %d", spy.count())
	}
	w.run("p", boom) // 3rd strike
	if spy.count() != 1 || spy.calls[0] != "p" {
		t.Fatalf("expected disable after 3 strikes; got %v", spy.calls)
	}
}

func TestWatchdogSuccessClearsStrikes(t *testing.T) {
	spy := &disableSpy{}
	w := newWatchdog(time.Second, 2, spy.fn)
	boom := func() error { return errors.New("boom") }
	ok := func() error { return nil }

	w.run("p", boom) // strike 1
	w.run("p", ok)   // clears
	w.run("p", boom) // strike 1 again
	if spy.count() != 0 {
		t.Fatalf("a success between panics must reset strikes; got %d disables", spy.count())
	}
}

func TestWatchdogPanicCountsAsStrike(t *testing.T) {
	spy := &disableSpy{}
	w := newWatchdog(time.Second, 1, spy.fn)
	err := w.run("p", func() error { panic("kaboom") })
	if err == nil || spy.count() != 1 {
		t.Fatalf("a panicking dispatch must be recovered and count as a strike; err=%v disables=%d", err, spy.count())
	}
}
```

- [ ] **Step 2: Run — fails (newWatchdog undefined)**

Run: `CGO_ENABLED=1 go test -tags fts5 ./internal/script/ -run TestWatchdog -v`
Expected: FAIL.

- [ ] **Step 3: Implement `internal/script/watchdog.go`**

```go
package script

import (
	"fmt"
	"sync"
	"time"

	"github.com/matt0x6f/irc-client/internal/extension"
)

const (
	defaultDispatchDeadline = 2 * time.Second
	defaultMaxStrikes       = 3
)

// watchdog bounds a script's dispatch. A dispatch exceeding the deadline (a hang
// or infinite loop) disables the script immediately; consecutive errors/panics
// disable it after maxStrikes; a success clears the count.
//
// HONEST LIMIT: Go cannot preempt a goroutine, so on a deadline overrun the
// dispatch goroutine LEAKS — we disable the script (stopping further dispatch)
// but cannot reclaim the spinning goroutine. We contain blast radius, not kill.
type watchdog struct {
	deadline   time.Duration
	maxStrikes int
	disable    func(id extension.ID, reason string)

	mu      sync.Mutex
	strikes map[extension.ID]int
}

func newWatchdog(deadline time.Duration, maxStrikes int, disable func(extension.ID, string)) *watchdog {
	return &watchdog{
		deadline:   deadline,
		maxStrikes: maxStrikes,
		disable:    disable,
		strikes:    make(map[extension.ID]int),
	}
}

// run executes dispatch under the deadline + strike policy and returns the
// dispatch error (or a deadline error). Panics inside dispatch are recovered and
// returned as errors (counted as strikes).
func (w *watchdog) run(id extension.ID, dispatch func() error) error {
	done := make(chan error, 1) // buffered: a late goroutine never blocks on send
	go func() {
		defer func() {
			if r := recover(); r != nil {
				done <- fmt.Errorf("panic: %v", r)
			}
		}()
		done <- dispatch()
	}()

	select {
	case err := <-done:
		w.record(id, err)
		return err
	case <-time.After(w.deadline):
		w.disable(id, fmt.Sprintf("dispatch exceeded %s deadline", w.deadline))
		return fmt.Errorf("script %s: dispatch exceeded %s deadline", id, w.deadline)
	}
}

// record updates the strike count; disable is called (outside the lock) once the
// count reaches maxStrikes.
func (w *watchdog) record(id extension.ID, err error) {
	if err == nil {
		w.mu.Lock()
		delete(w.strikes, id)
		w.mu.Unlock()
		return
	}
	w.mu.Lock()
	w.strikes[id]++
	n := w.strikes[id]
	hit := n >= w.maxStrikes
	if hit {
		delete(w.strikes, id)
	}
	w.mu.Unlock()
	if hit {
		w.disable(id, fmt.Sprintf("disabled after %d consecutive errors: %v", n, err))
	}
}
```

- [ ] **Step 4: Green (incl -race) + commit**

Run: `CGO_ENABLED=1 go test -race -tags fts5 ./internal/script/ -run TestWatchdog -v`
```bash
git add internal/script/watchdog.go internal/script/watchdog_test.go
git commit -m "feat(script): watchdog core (dispatch deadline + strike policy)"
```

---

## Task 2: Self-message (echo-loop) guard

**Files:**
- Modify: `internal/script/manager.go` (`NewManager` gains `selfNick`; `buildTextEvent` skips self)
- Modify: `internal/script/manager_test.go` (update `NewManager` callers; add self-skip test)
- Modify: `app.go` (pass a `selfNick` closure)

**Interfaces:**
- Changed: `func NewManager(bus *events.EventBus, dir string, sender Sender, selfNick func(networkID int64) string) *Manager`. `buildTextEvent` returns `false` when `nick == selfNick(networkID)`.

- [ ] **Step 1: Update the test fixture helper + add the failing test**

Every existing `NewManager(bus, dir, sender)` call in `manager_test.go` gains a 4th arg. Use a helper default to minimize churn — add near the top of the test file:
```go
func noSelf(int64) string { return "" }
```
and change existing calls to `NewManager(bus, dir, fs.send, noSelf)` / `NewManager(events.NewEventBus(), dir, fn, noSelf)`.

Add the self-skip test:
```go
func TestManagerSkipsOwnMessages(t *testing.T) {
	fs := &fakeSender{}
	bus := events.NewEventBus()
	// selfNick says the bot is "mybot" on network 1.
	m := NewManager(bus, "testdata", fs.send, func(networkID int64) string {
		if networkID == 1 {
			return "mybot"
		}
		return ""
	})
	_ = m.LoadAll()

	// A message whose sender IS the bot (its own echo) must not be delivered.
	bus.EmitSync(msgEvent(1, "#c", "mybot", "!hello", ""))

	fs.mu.Lock()
	defer fs.mu.Unlock()
	if len(fs.sent) != 0 {
		t.Fatalf("expected no reply to the bot's own message; got %+v", fs.sent)
	}
}
```

- [ ] **Step 2: Run — fails (NewManager arity)**

Run: `CGO_ENABLED=1 go test -tags fts5 ./internal/script/ -run TestManagerSkipsOwnMessages -v`
Expected: FAIL (compile: too few args, until you update NewManager + callers).

- [ ] **Step 3: Implement**

In `manager.go`, add the field + constructor param:
```go
type Manager struct {
	dir      string
	sender   Sender
	selfNick func(networkID int64) string
	// ... existing fields
}

func NewManager(bus *events.EventBus, dir string, sender Sender, selfNick func(networkID int64) string) *Manager {
	// ... set m.selfNick = selfNick alongside the others
}
```
In `buildTextEvent`, after reading `nick`/`networkID` and the existing `nick == "" || nick == "*"` guard, add:
```go
	if m.selfNick != nil && nick == m.selfNick(networkID) {
		return cascade.TextEvent{}, false // the bot's own (possibly echoed) message
	}
```

- [ ] **Step 4: Wire app.go**

Where the manager is constructed (`a.scriptMgr = script.NewManager(eventBus, scriptDir, a.SendMessage)`), add the selfNick closure:
```go
	a.scriptMgr = script.NewManager(eventBus, scriptDir, a.SendMessage, func(networkID int64) string {
		nick, _ := a.GetCurrentNick(networkID)
		return nick
	})
```

- [ ] **Step 5: Verify (stub dist) + commit**

```bash
mkdir -p frontend/dist && printf '<!doctype html>' > frontend/dist/index.html
go build .
CGO_ENABLED=1 go test -race -tags fts5 ./internal/script/ -count=1
gofmt -l app.go internal/script/
```
Expected: app builds; script green under -race; gofmt empty. (Do NOT commit frontend/dist.)
```bash
git add app.go internal/script/manager.go internal/script/manager_test.go
git commit -m "feat(script): skip the bot's own messages (echo-loop guard)"
```

---

## Task 3: Compose the watchdog into the Manager

**Files:**
- Modify: `internal/script/manager.go` (`watchdog` field; `NewManager` builds it; `Deliver` → `dispatch` under the watchdog; `disableScript`)
- Modify: `internal/script/manager_test.go` (auto-disable-after-strikes integration test)

**Interfaces:**
- Changed: `Manager` gains `watchdog *watchdog`; `NewManager` sets `m.watchdog = newWatchdog(defaultDispatchDeadline, defaultMaxStrikes, m.disableScript)`. `Deliver` delegates to `m.watchdog.run(id, func() error { return m.dispatch(id, ev) })`. `dispatch` is the former `Deliver` body. Adds `func (m *Manager) disableScript(id extension.ID, reason string)`.

- [ ] **Step 1: Write the failing integration test**

```go
// add to internal/script/manager_test.go — uses the existing panicker fixture (panics every dispatch).
func TestManagerAutoDisablesRepeatPanicker(t *testing.T) {
	fs := &fakeSender{}
	bus := events.NewEventBus()
	m := NewManager(bus, "testdata", fs.send, noSelf)
	_ = m.LoadAll()

	// defaultMaxStrikes consecutive panics must auto-disable the panicker.
	for i := 0; i < defaultMaxStrikes; i++ {
		bus.EmitSync(msgEvent(1, "#c", "bob", "anything", ""))
	}

	ext, ok := m.registry().Get(extension.ID("panicker"))
	if !ok || ext.Enabled || ext.Status != extension.StatusRunaway {
		t.Fatalf("panicker should be auto-disabled (runaway); got %+v ok=%v", ext, ok)
	}
}
```

- [ ] **Step 2: Run — fails (panicker stays StatusError/enabled without the watchdog)**

Run: `CGO_ENABLED=1 go test -tags fts5 ./internal/script/ -run TestManagerAutoDisables -v`
Expected: FAIL (panicker not disabled yet).

- [ ] **Step 3: Restructure Deliver + add disableScript**

Rename the current `Deliver` body to `dispatch` (keep the per-script mutex + the inner `recover` so the mutex unlocks correctly on panic), and make `Deliver` delegate to the watchdog:
```go
// Deliver implements extension.Host. The watchdog bounds each dispatch and
// auto-disables a script that hangs or repeatedly fails.
func (m *Manager) Deliver(id extension.ID, ev events.Event) error {
	return m.watchdog.run(id, func() error { return m.dispatch(id, ev) })
}

// dispatch translates the event and runs the script's handler, serialized per
// script. A handler panic is recovered (so the per-script mutex unlocks) and
// returned as an error for the watchdog to count.
func (m *Manager) dispatch(id extension.ID, ev events.Event) (err error) {
	// ... the EXISTING Deliver body verbatim (lookup, recover defer, switch, l.mu, DispatchText)
}

// disableScript is the watchdog's disable callback: stop delivery and record why.
func (m *Manager) disableScript(id extension.ID, reason string) {
	m.reg.SetEnabled(id, false)
	m.reg.SetStatus(id, extension.StatusRunaway, reason)
	logger.Log.Warn().Str("script", string(id)).Str("reason", reason).Msg("script auto-disabled by watchdog")
}
```
In `NewManager`, after constructing `m`, set:
```go
	m.watchdog = newWatchdog(defaultDispatchDeadline, defaultMaxStrikes, m.disableScript)
```
Add the `watchdog *watchdog` field and the `github.com/matt0x6f/irc-client/internal/logger` import (match how other packages log — grep `logger.Log` usage).

- [ ] **Step 4: Green (incl -race) + commit**

Run:
```bash
CGO_ENABLED=1 go test -race -tags fts5 ./internal/script/ -count=1
CGO_ENABLED=1 go test -tags fts5 ./internal/... -count=1
gofmt -l internal/script/
```
Expected: all green; the panicker auto-disables after `defaultMaxStrikes`; gofmt empty.
```bash
git add internal/script/manager.go internal/script/manager_test.go
git commit -m "feat(script): compose watchdog into dispatch (auto-disable runaways)"
```

---

## PR6 Self-Review

- **Spec coverage:** deadline → disable ✓ (T1 unit, controllable block); strike policy → disable after N ✓ (T1 + T3 integration via panicker); panic recovered + counted ✓ (T1/T3); echo-loop guard (skip self) ✓ (T2); auto-disable sets `Enabled=false` + `StatusRunaway` so the Router stops delivering ✓ (T3).
- **Honest limit documented:** deadline overrun leaks the goroutine (can't kill); no real-infinite-loop fixture run in tests.
- **Safety preserved:** per-script serialization + panic recovery still present (recover relocated into `dispatch`); the watchdog's extra recover is a defensive backstop.
- **Type consistency:** `disableScript` signature == watchdog's `disable` callback; `selfNick` closure == `func(int64) string`.

## After PR6 — remaining deferred work

| Deferred | Where |
|----------|-------|
| **More cascade actions** (Say/Join/Part/Notice, timers) + **more event types** (OnJoin/OnPart/**OnNotice** + the `messageType` producer discriminator) | **PR7** |
| **Frontend management & permissions surface** + **enable/disable persistence** | **PR8** |
| **Configurable** deadline/maxStrikes (per-user settings) + memory-pressure monitoring | **PR8/v2** (PR6 ships sensible constant defaults) |
| **Permission enforcement** (sandbox tiering) + harden `parseManifest` to the leading comment block | **v2** |
| **Cut the `cascade/v1.0.0` tag** | **Release** |
