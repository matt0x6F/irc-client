# Cascade Scripting v1 — PR4: Script host/manager (scripts run against live IRC)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Make scripts actually run: discover script directories, load them via the PR1 engine, register them in the PR2 `extension.Registry`, and implement `extension.Host.Deliver` so a real `message.received` IRC event is translated into a `cascade.TextEvent` and dispatched to a script's `OnText` — including a working `Reply` back to IRC. Wire it into the App.

**Architecture:** `script.Manager` is the `extension.Host` for scripts. The PR2 `extension.Router` (attached to the event bus) fans bus events to the Manager's `Deliver`, which translates `message.received` → `cascade.TextEvent` (with a `Reply` closure that calls an App-provided sender) → `Script.DispatchText`. Dispatch is serialized per script (Yaegi interpreters are not safe for concurrent `Call`) and wrapped in panic recovery (a buggy script must not crash the app). The App constructs the Manager with `~/.cascade-chat/scripts` and `App.SendMessage` as the sender.

**Tech Stack:** Go; the PR1 engine (`internal/script`), the PR2 core (`internal/extension`), the event bus (`internal/events`), the cascade SDK module (`cascade/`).

## Global Constraints

- **Module path lowercase** `github.com/matt0x6f/irc-client`; cascade SDK at `github.com/matt0x6f/irc-client/cascade`.
- **Go tests:** `CGO_ENABLED=1 go test -tags fts5 <pkg> -count=1`; the `internal/script` package is concurrency-bearing → also run `-race`. gofmt-clean; `go vet` passes.
- **Panic recovery is NOT deferred:** `Deliver` must `recover()` so a panicking script is marked errored, never crashing the host. (The full deadline/runaway/memory watchdog IS deferred — see After PR4.)
- **Serialize dispatch per script:** concurrent `Call` into one Yaegi interpreter is unsafe; the Manager guards each script's dispatch with a per-script mutex.
- **Reuse, don't reinvent:** the event is `EventMessageReceived` (`"message.received"`, defined in `internal/irc/events.go`); the sender is `App.SendMessage(networkID int64, target, message string) error` (`app_commands.go`). Do not add a parallel send path.
- **cascade is still pre-release:** the `cascade/v1.0.0` tag is not cut yet, so extending the cascade API here (adding `TextEvent.Channel`) is fine — finalize the v1 surface before release.

## Event + sender facts (grounded)

- `EventMessageReceived = "message.received"` (`internal/irc/events.go`). Data: `map[string]interface{}{ "network": string(address), "networkId": int64, "channel": string, "user": string, "message": string, "pmTarget": string }`. For a channel message `channel` is `#foo`; for a DM `channel` is our own nick and `pmTarget` is the peer.
- Reply target: a channel message replies to `channel`; a DM replies to `user` (the sender).
- `App.SendMessage(networkID int64, target, message string) error` — the sender to inject.
- Scripts dir: `filepath.Join(baseDir, "scripts")` where `baseDir` is `~/.cascade-chat` (or `$CASCADE_DATA_DIR`), mirroring `pluginDir` at `app.go:100`.

---

## PR4 — File Structure

- `cascade/events.go` — add `Channel` field + `IsDM()`; widen `NewTextEvent` (Task 1).
- `cascade/events_test.go` + `internal/script/engine_test.go` — update `NewTextEvent` call sites (Task 1).
- `internal/script/manager.go` — `Manager` (discovery, load, register, `Host.Deliver`, reply routing, per-script serialization, panic recovery) (Task 2).
- `internal/script/manager_test.go` — Manager tests with a fake sender + real bus (Task 2).
- `internal/script/testdata/panicker/panicker.go` — fixture whose `OnText` panics (Task 2).
- `app.go` (+ App struct field) — construct + wire + `LoadAll` on startup (Task 3).

---

## Task 1: cascade.TextEvent gains `Channel` + `IsDM`

**Files:**
- Modify: `cascade/events.go`, `cascade/events_test.go`
- Modify: `internal/script/engine_test.go` (existing `NewTextEvent` callers)

**Interfaces:**
- Produces: `TextEvent` gains `Channel string`; `NewTextEvent(nick, channel, message string, reply func(string)) TextEvent`; `func (e TextEvent) IsDM() bool` (true when `Channel` is not a channel name, i.e. doesn't start with `#` or `&`).

- [ ] **Step 1: Update the cascade events test for the new shape**

Replace the body of `cascade/events_test.go`'s test to construct with a channel and assert `Channel`/`IsDM`:
```go
func TestTextEventReplyRoutesToSink(t *testing.T) {
	var got []string
	e := NewTextEvent("alice", "#chan", "!hello there", func(s string) { got = append(got, s) })

	if e.Channel != "#chan" || e.IsDM() {
		t.Fatalf("Channel=%q IsDM=%v; want #chan / false", e.Channel, e.IsDM())
	}
	if !e.HasPrefix("!hello") || e.Arg(2) != "there" {
		t.Fatalf("HasPrefix/Arg wrong: %v %q", e.HasPrefix("!hello"), e.Arg(2))
	}
	e.Reply("hi alice")
	if len(got) != 1 || got[0] != "hi alice" {
		t.Fatalf("reply sink = %v; want [hi alice]", got)
	}

	dm := NewTextEvent("bob", "mynick", "yo", nil)
	if !dm.IsDM() {
		t.Fatalf("expected IsDM true for non-channel target %q", dm.Channel)
	}
}
```

- [ ] **Step 2: Run it — fails to compile (NewTextEvent arity, Channel/IsDM undefined)**

Run: `(cd cascade && CGO_ENABLED=1 go test ./... -run TestTextEventReplyRoutesToSink -v)`
Expected: FAIL (compile: too few args / undefined Channel / undefined IsDM).

- [ ] **Step 3: Implement in `cascade/events.go`**

```go
type TextEvent struct {
	Nick    string
	Channel string // "#chan" for a channel message; the bot's own nick for a DM
	Message string

	replyFn func(string)
}

// NewTextEvent is the host-side constructor. Scripts never call it.
func NewTextEvent(nick, channel, message string, reply func(string)) TextEvent {
	return TextEvent{Nick: nick, Channel: channel, Message: message, replyFn: reply}
}

// IsDM reports whether this message was a direct message (Channel is not a channel name).
func (e TextEvent) IsDM() bool {
	return e.Channel == "" || (e.Channel[0] != '#' && e.Channel[0] != '&')
}
```
Keep `HasPrefix`, `Arg`, `Reply` unchanged.

- [ ] **Step 4: Fix the other `NewTextEvent` callers in `internal/script/engine_test.go`**

Every `cascade.NewTextEvent("x", "msg", fn)` becomes `cascade.NewTextEvent("x", "#chan", "msg", fn)` (insert a channel arg — use `"#chan"` for the existing channel-style cases; the assertions on the reply text are unchanged). Find each call and add the channel argument.

- [ ] **Step 5: Verify both modules green**

Run:
```bash
(cd cascade && CGO_ENABLED=1 go test ./... -count=1)
CGO_ENABLED=1 go test -tags fts5 ./internal/script/... -count=1
```
Expected: both PASS. (No symbol-table regen is required: the committed symbols register the `TextEvent` *type*, so new fields/methods on it are picked up via reflection automatically. If you wish, run `task script-symbols` and confirm it produces no diff.)

- [ ] **Step 6: Commit**

```bash
git add cascade/ internal/script/engine_test.go
git commit -m "feat(cascade): add TextEvent.Channel + IsDM for reply routing"
```

---

## Task 2: `script.Manager` — discover, load, deliver, reply

**Files:**
- Create: `internal/script/manager.go`
- Create: `internal/script/manager_test.go`
- Create: `internal/script/testdata/panicker/panicker.go`

**Interfaces:**
- Consumes: `LoadPackage`, `Script`, `Script.Has`, `Script.DispatchText` (PR1); `extension.{Registry,Router,Host,Extension,ID,Kind*,Status*}` (PR2); `events.{Event,EventBus}`; `cascade.{TextEvent,NewTextEvent}` (Task 1).
- Produces:
  - `type Sender func(networkID int64, target, message string) error`.
  - `type Manager struct { ... }`; `func NewManager(bus *events.EventBus, dir string, sender Sender) *Manager` (creates a `Registry` + `Router`, attaches the router to `bus`).
  - `func (m *Manager) LoadAll() error` — ensures `dir` exists, then loads every immediate subdirectory containing `.go` files.
  - `func (m *Manager) Kind() extension.Kind` and `func (m *Manager) Deliver(id extension.ID, ev events.Event) error` — implements `extension.Host`.
  - `func (m *Manager) List() []extension.Extension` (delegates to the registry; for PR8 UI later).
  - The event type constant string used: `"message.received"` (matches `irc.EventMessageReceived`).

- [ ] **Step 1: Write the panicker fixture**

```go
// internal/script/testdata/panicker/panicker.go
package main

import "github.com/matt0x6f/irc-client/cascade"

func OnText(e cascade.TextEvent) {
	panic("boom")
}
```

- [ ] **Step 2: Write the failing Manager test**

```go
// internal/script/manager_test.go
package script

import (
	"sync"
	"testing"

	"github.com/matt0x6f/irc-client/internal/events"
	"github.com/matt0x6f/irc-client/internal/extension"
)

// fakeSender records Reply sends.
type fakeSender struct {
	mu   sync.Mutex
	args [][3]string // networkID(as text not needed)->we store target+message; keep simple
	sent []sentMsg
}
type sentMsg struct {
	network int64
	target  string
	message string
}

func (f *fakeSender) send(networkID int64, target, message string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, sentMsg{networkID, target, message})
	return nil
}

func msgEvent(networkID int64, channel, user, message, pmTarget string) events.Event {
	return events.Event{
		Type: "message.received",
		Data: map[string]interface{}{
			"network":   "irc.example",
			"networkId": networkID,
			"channel":   channel,
			"user":      user,
			"message":   message,
			"pmTarget":  pmTarget,
		},
		Source: events.EventSourceIRC,
	}
}

func TestManagerChannelMessageReplies(t *testing.T) {
	fs := &fakeSender{}
	bus := events.NewEventBus()
	m := NewManager(bus, "testdata", fs.send) // loads each subdir of testdata as a script
	if err := m.LoadAll(); err != nil {
		t.Fatalf("LoadAll: %v", err)
	}

	// greeter (PR1 fixture) replies "Hi <nick>" to the channel.
	bus.EmitSync(msgEvent(7, "#chan", "bob", "!hello world", ""))

	fs.mu.Lock()
	defer fs.mu.Unlock()
	found := false
	for _, s := range fs.sent {
		if s.network == 7 && s.target == "#chan" && s.message == "Hi bob" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected greeter to reply 'Hi bob' to #chan on net 7; got %+v", fs.sent)
	}
}

func TestManagerDMRepliesToSender(t *testing.T) {
	fs := &fakeSender{}
	bus := events.NewEventBus()
	m := NewManager(bus, "testdata", fs.send)
	_ = m.LoadAll()

	// DM: channel is our own nick (non-channel), so reply target must be the sender.
	bus.EmitSync(msgEvent(1, "mynick", "carol", "!hello", "carol"))

	fs.mu.Lock()
	defer fs.mu.Unlock()
	for _, s := range fs.sent {
		if s.message == "Hi carol" && s.target != "carol" {
			t.Fatalf("DM reply target = %q; want carol (the sender)", s.target)
		}
	}
}

func TestManagerPanicIsRecoveredAndMarksError(t *testing.T) {
	fs := &fakeSender{}
	bus := events.NewEventBus()
	m := NewManager(bus, "testdata", fs.send)
	_ = m.LoadAll()

	// The panicker fixture panics in OnText; this must not crash the test process.
	bus.EmitSync(msgEvent(1, "#chan", "dave", "anything", ""))

	ext, ok := m.registry().Get(extension.ID("panicker"))
	if !ok || ext.Status != extension.StatusError {
		t.Fatalf("panicker status = %+v, %v; want StatusError", ext, ok)
	}
}
```
(If exposing the registry via an unexported `registry()` helper is undesirable, assert via `m.List()` and find the panicker's `Status` instead — pick one and keep it; the point is to observe the errored status.)

- [ ] **Step 3: Run it — fails (NewManager undefined)**

Run: `CGO_ENABLED=1 go test -tags fts5 ./internal/script/ -run TestManager -v`
Expected: FAIL — undefined `NewManager`.

- [ ] **Step 4: Implement `internal/script/manager.go`**

```go
package script

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/matt0x6f/irc-client/cascade"
	"github.com/matt0x6f/irc-client/internal/events"
	"github.com/matt0x6f/irc-client/internal/extension"
)

// eventMessageReceived mirrors irc.EventMessageReceived without importing the irc
// package (avoids an import cycle; the string is the contract).
const eventMessageReceived = "message.received"

// Sender sends a message to target on the given network (e.g. App.SendMessage).
type Sender func(networkID int64, target, message string) error

// loaded is a loaded script plus a mutex serializing its dispatch (Yaegi
// interpreters are not safe for concurrent Call).
type loaded struct {
	script *Script
	mu     sync.Mutex
}

// Manager discovers, loads, and dispatches scripts. It is the extension.Host
// for the script runtime.
type Manager struct {
	dir    string
	sender Sender
	reg    *extension.Registry
	router *extension.Router

	mu      sync.RWMutex
	scripts map[extension.ID]*loaded
}

// NewManager builds a script manager and attaches its router to the bus.
func NewManager(bus *events.EventBus, dir string, sender Sender) *Manager {
	reg := extension.NewRegistry()
	m := &Manager{
		dir:     dir,
		sender:  sender,
		reg:     reg,
		router:  extension.NewRouter(reg),
		scripts: make(map[extension.ID]*loaded),
	}
	m.router.Attach(bus)
	return m
}

// LoadAll ensures the scripts dir exists, then loads each immediate subdirectory
// that contains .go files. Per-script load failures are recorded as errored
// extensions, never aborting the whole scan.
func (m *Manager) LoadAll() error {
	if err := os.MkdirAll(m.dir, 0o755); err != nil {
		return fmt.Errorf("scripts dir: %w", err)
	}
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		return fmt.Errorf("read scripts dir: %w", err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		sub := filepath.Join(m.dir, e.Name())
		if goFiles, _ := filepath.Glob(filepath.Join(sub, "*.go")); len(goFiles) == 0 {
			continue
		}
		m.loadDir(sub)
	}
	return nil
}

func (m *Manager) loadDir(dir string) {
	id := extension.ID(filepath.Base(dir))
	ext := &extension.Extension{ID: id, Name: filepath.Base(dir), Kind: extension.KindScript, Enabled: true, Status: extension.StatusLoaded}

	s, err := LoadPackage(dir)
	if err != nil {
		ext.Status = extension.StatusError
		ext.Err = err.Error()
		m.reg.Register(ext, m, nil) // registered (visible) but no subscriptions
		return
	}

	var evs []string
	if s.Has("OnText") {
		evs = append(evs, eventMessageReceived)
	}

	m.mu.Lock()
	m.scripts[id] = &loaded{script: s}
	m.mu.Unlock()
	m.reg.Register(ext, m, evs)
}

// Kind implements extension.Host.
func (m *Manager) Kind() extension.Kind { return extension.KindScript }

// Deliver implements extension.Host: translate the event and dispatch to the
// script, serialized per script and with panic recovery so a buggy script can
// never crash the host.
func (m *Manager) Deliver(id extension.ID, ev events.Event) (err error) {
	m.mu.RLock()
	l, ok := m.scripts[id]
	m.mu.RUnlock()
	if !ok {
		return nil
	}

	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("script %s panicked: %v", id, r)
		}
	}()

	switch ev.Type {
	case eventMessageReceived:
		te, ok := m.buildTextEvent(ev)
		if !ok {
			return nil
		}
		l.mu.Lock()
		defer l.mu.Unlock()
		l.script.DispatchText(te)
	}
	return nil
}

// buildTextEvent maps a message.received event into a cascade.TextEvent with a
// Reply closure routed to the right network + target.
func (m *Manager) buildTextEvent(ev events.Event) (cascade.TextEvent, bool) {
	nick, _ := ev.Data["user"].(string)
	channel, _ := ev.Data["channel"].(string)
	message, _ := ev.Data["message"].(string)
	networkID, _ := ev.Data["networkId"].(int64)
	if nick == "" {
		return cascade.TextEvent{}, false
	}

	target := channel
	if !isChannel(channel) {
		target = nick // DM: reply to the sender
	}
	reply := func(msg string) { _ = m.sender(networkID, target, msg) }
	return cascade.NewTextEvent(nick, channel, message, reply), true
}

func isChannel(s string) bool {
	return len(s) > 0 && (s[0] == '#' || s[0] == '&')
}

// List returns a snapshot of all registered scripts.
func (m *Manager) List() []extension.Extension { return m.reg.List() }

// registry exposes the registry for tests.
func (m *Manager) registry() *extension.Registry { return m.reg }
```

- [ ] **Step 5: Run the Manager tests (incl. -race)**

Run:
```bash
CGO_ENABLED=1 go test -tags fts5 ./internal/script/ -run TestManager -v
CGO_ENABLED=1 go test -race -tags fts5 ./internal/script/ -count=1
```
Expected: PASS (channel reply, DM reply-to-sender, panic-recovered-and-errored) and clean under `-race`.

- [ ] **Step 6: Commit**

```bash
git add internal/script/manager.go internal/script/manager_test.go internal/script/testdata/panicker/
git commit -m "feat(script): Manager wires engine as extension.Host (deliver, reply, panic recovery)"
```

---

## Task 3: Wire the script Manager into the App

**Files:**
- Modify: `app.go` (App struct field + construction + startup `LoadAll`)

**Interfaces:**
- Consumes: `script.NewManager`, `(*script.Manager).LoadAll` (Task 2); `App.SendMessage` (existing); `eventBus`, `baseDir` (existing in the constructor).

> **Build note:** the root `package main` embeds `frontend/dist`, which is absent in a fresh worktree, so `go build .` fails until a dist exists. For verification only, create a stub: `mkdir -p frontend/dist && printf '<!doctype html>' > frontend/dist/index.html` (it's git-ignored — do NOT commit it). Then `go build .` compiles.

- [ ] **Step 1: Add the field + construct + load**

In `app.go`: add a field to the `App` struct:
```go
	scriptMgr *script.Manager
```
In the constructor, near `pluginDir := filepath.Join(baseDir, "plugins")` (~line 100), after the App value `a` exists and `eventBus` is available, add:
```go
	scriptDir := filepath.Join(baseDir, "scripts")
	a.scriptMgr = script.NewManager(eventBus, scriptDir, a.SendMessage)
```
And at the point where plugins are loaded/started during startup, load scripts too:
```go
	if err := a.scriptMgr.LoadAll(); err != nil {
		logger.Log.Warn().Err(err).Msg("failed to load scripts")
	}
```
(Place `a.scriptMgr = ...` where `a` and `eventBus` are both in scope — if the constructor builds `a` after computing dirs, assign there; otherwise assign right after `a` is constructed. Match the existing plugin-manager wiring location. Add the `script` import.)

- [ ] **Step 2: Build the app (with a stub dist) + vet**

Run:
```bash
mkdir -p frontend/dist && printf '<!doctype html>' > frontend/dist/index.html
go build .
CGO_ENABLED=1 go vet -tags fts5 ./internal/script/...
gofmt -l app.go internal/script/ cascade/
```
Expected: `go build .` succeeds (app compiles with the wiring); vet clean; gofmt empty. (Leave `frontend/dist` uncommitted.)

- [ ] **Step 3: Full suite**

Run: `CGO_ENABLED=1 go test -tags fts5 ./internal/... ./cascade/... -count=1`
Expected: all green.

- [ ] **Step 4: Commit (app.go only — NOT frontend/dist)**

```bash
git add app.go
git commit -m "feat(app): construct + load the script manager on startup"
```

---

## PR4 Self-Review

- **Spec coverage:** `TextEvent.Channel`/`IsDM` ✓ (T1); discovery + load + register ✓ (T2); `Host.Deliver` translate `message.received`→`TextEvent`→`DispatchText` ✓ (T2); Reply routed to channel/sender via injected sender ✓ (T2); per-script dispatch serialization ✓ (T2); panic recovery → errored status ✓ (T2); App wiring + startup load ✓ (T3).
- **Safety floors kept (not deferred):** panic recovery; concurrent-dispatch serialization.
- **Type consistency:** `Sender` signature == `App.SendMessage`; event string `"message.received"` == `irc.EventMessageReceived`; `cascade.NewTextEvent` 4-arg form used everywhere.

## After PR4 — deferred work (tracked so it IS picked up)

| Deferred | Where it lands |
|----------|----------------|
| `// cascade:` **manifest parsing** (name + permissions); must parse BEFORE the package merge (the merge strips file-level comments — PR1 finding) | **PR5** |
| **File-watch hot reload** (fsnotify) + **reload/enable/disable** lifecycle | **PR5** |
| **Enable/disable persistence** (SQLite) so disabled scripts stay disabled across restarts | **PR5** |
| **Scripts-dir `go.mod` scaffolding** (`require …/cascade v1.0.0`) so external gopls resolves the SDK | **PR5** |
| **Full watchdog**: per-dispatch deadline, runaway detection, memory pressure, auto-disable after N strikes (PR4 has panic recovery only) | **PR6** |
| **More cascade actions** (`Say`/`Join`/`Part`/`Notice`, timers `Every`/`After`) + **more event types/handlers** (`OnJoin`/`OnPart`/`OnKick`/…) | **PR7** |
| **Frontend management & permissions surface** (Wails-bound methods + Scripts panel) | **PR8** |
| **Cut the `cascade/v1.0.0` git tag** (path-prefixed) once the v1 cascade API is final | **Release** |
