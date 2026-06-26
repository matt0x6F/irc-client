# Cascade Scripting v1 — PR2: Shared `extension/` core

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Build the runtime-agnostic `internal/extension/` core — extension identity/registry, the `Host.Deliver` seam, and an event Router that fans bus events out to the enabled extensions that subscribed to each event's type.

**Architecture:** A `Registry` holds each extension's identity/status plus its `Host` and the event types it subscribed to. A `Router` implements `events.Subscriber`, subscribes to the bus wildcard (`"*"`), and on each event delivers to the enabled, subscribed extensions via `Host.Deliver`. Plugins and scripts will both plug in as `Host`s in later PRs; PR2 proves the seam with a fake host. No IRC wiring, no UI, no real script host yet.

**Tech Stack:** Go; the existing event bus (`internal/events/bus.go`).

## Global Constraints

- **Go tests:** `CGO_ENABLED=1 go test -tags fts5 <pkg> -count=1`. The new `internal/extension` package is concurrency-bearing — also run it under the race detector: `CGO_ENABLED=1 go test -race -tags fts5 ./internal/extension/ -count=1`.
- **gofmt clean** (`gofmt -l` empty) and **`go vet`** pass.
- **Reuse, don't duplicate, the event type.** Deliver and Router operate on `events.Event` (`internal/events`), not a parallel event struct.
- **Module path:** `github.com/matt0x6f/irc-client`. New package import path: `github.com/matt0x6f/irc-client/internal/extension`.
- **Leave the plugin system untouched.** PR2 is greenfield `extension/`; it does NOT modify `internal/plugin/`. Plugins adopt this core in a later PR.
- **No generics** in the public surface (consistency with the script host; keeps the seam simple).

---

## Event bus facts (from `internal/events/bus.go`)

- `type Event struct { Type string; Data map[string]interface{}; Timestamp time.Time; Source EventSource }`
- `type Subscriber interface { OnEvent(event Event) }`
- `func (eb *EventBus) Subscribe(eventType string, subscriber Subscriber)` — `"*"` is a wildcard matching every event.
- `func (eb *EventBus) EmitSync(event Event)` — synchronous delivery; **use this in tests** for determinism (`Emit` dispatches each subscriber on its own goroutine).

---

## PR2 — File Structure

- `internal/extension/extension.go` — `Kind`, `Status`, `ID`, `Extension` (identity/status value types).
- `internal/extension/host.go` — the `Host` interface (the runtime seam).
- `internal/extension/registry.go` — `Registry` (register/list/get/enable/status + recipient lookup).
- `internal/extension/registry_test.go` — registry unit tests.
- `internal/extension/router.go` — `Router` (bus subscriber; fan-out via Host).
- `internal/extension/router_test.go` — router + seam tests (fake host).

---

## PR2 — Tasks

### Task 1: Extension identity types + Registry

**Files:**
- Create: `internal/extension/extension.go`
- Create: `internal/extension/host.go`
- Create: `internal/extension/registry.go`
- Test: `internal/extension/registry_test.go`

**Interfaces:**
- Produces:
  - `type Kind string` (`KindScript`, `KindPlugin`); `type Status string` (`StatusLoaded`, `StatusDisabled`, `StatusError`, `StatusRunaway`); `type ID string`.
  - `type Extension struct { ID ID; Name string; Kind Kind; Enabled bool; Status Status; Perms []string; Err string }`.
  - `type Host interface { Kind() Kind; Deliver(id ID, ev events.Event) error }`.
  - `type Registry` with: `NewRegistry() *Registry`; `Register(ext *Extension, host Host, eventTypes []string)`; `Get(id ID) (*Extension, bool)`; `List() []*Extension`; `SetEnabled(id ID, enabled bool)`; `SetStatus(id ID, status Status, errMsg string)`; and an unexported `recipients(eventType string) []deliverable` used by the Router (Task 2), where `type deliverable struct { id ID; host Host }`.

- [ ] **Step 1: Write the failing registry test**

```go
// internal/extension/registry_test.go
package extension

import (
	"testing"

	"github.com/matt0x6f/irc-client/internal/events"
)

// nopHost is a minimal Host used where delivery behavior is irrelevant.
type nopHost struct{}

func (nopHost) Kind() Kind                              { return KindScript }
func (nopHost) Deliver(id ID, ev events.Event) error   { return nil }

func TestRegistryRegisterGetList(t *testing.T) {
	r := NewRegistry()
	ext := &Extension{ID: "greeter", Name: "Greeter", Kind: KindScript, Enabled: true, Status: StatusLoaded}
	r.Register(ext, nopHost{}, []string{"privmsg"})

	got, ok := r.Get("greeter")
	if !ok || got.ID != "greeter" || got.Name != "Greeter" {
		t.Fatalf("Get(greeter) = %+v, %v", got, ok)
	}
	if list := r.List(); len(list) != 1 || list[0].ID != "greeter" {
		t.Fatalf("List() = %+v; want one entry greeter", list)
	}
	if _, ok := r.Get("nope"); ok {
		t.Fatalf("Get(nope) should be false")
	}
}

func TestRegistrySetEnabledAndStatus(t *testing.T) {
	r := NewRegistry()
	ext := &Extension{ID: "x", Kind: KindScript, Enabled: true, Status: StatusLoaded}
	r.Register(ext, nopHost{}, nil)

	r.SetEnabled("x", false)
	got, _ := r.Get("x")
	if got.Enabled || got.Status != StatusDisabled {
		t.Fatalf("after disable: enabled=%v status=%v; want false/disabled", got.Enabled, got.Status)
	}

	r.SetEnabled("x", true)
	got, _ = r.Get("x")
	if !got.Enabled || got.Status != StatusLoaded {
		t.Fatalf("after enable: enabled=%v status=%v; want true/loaded", got.Enabled, got.Status)
	}

	r.SetStatus("x", StatusError, "boom")
	got, _ = r.Get("x")
	if got.Status != StatusError || got.Err != "boom" {
		t.Fatalf("after SetStatus: status=%v err=%q; want error/boom", got.Status, got.Err)
	}
}

func TestRegistryRecipientsFiltersByTypeAndEnabled(t *testing.T) {
	r := NewRegistry()
	r.Register(&Extension{ID: "a", Enabled: true, Status: StatusLoaded}, nopHost{}, []string{"privmsg"})
	r.Register(&Extension{ID: "b", Enabled: true, Status: StatusLoaded}, nopHost{}, []string{"join"})
	r.Register(&Extension{ID: "c", Enabled: false, Status: StatusDisabled}, nopHost{}, []string{"privmsg"})

	rcpts := r.recipients("privmsg")
	if len(rcpts) != 1 || rcpts[0].id != "a" {
		t.Fatalf("recipients(privmsg) = %+v; want only a", rcpts)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `CGO_ENABLED=1 go test -tags fts5 ./internal/extension/ -v`
Expected: FAIL — package/types undefined.

- [ ] **Step 3: Write the types**

```go
// internal/extension/extension.go
package extension

// Kind distinguishes the runtime backing an extension.
type Kind string

const (
	KindScript Kind = "script"
	KindPlugin Kind = "plugin"
)

// Status is an extension's lifecycle state.
type Status string

const (
	StatusLoaded   Status = "loaded"
	StatusDisabled Status = "disabled"
	StatusError    Status = "error"
	StatusRunaway  Status = "runaway"
)

// ID uniquely identifies an extension within its kind (e.g. a script directory name).
type ID string

// Extension is the runtime-agnostic identity and status of a loaded extension.
type Extension struct {
	ID      ID
	Name    string
	Kind    Kind
	Enabled bool
	Status  Status
	Perms   []string
	Err     string // last load/dispatch error; "" if none
}
```

```go
// internal/extension/host.go
package extension

import "github.com/matt0x6f/irc-client/internal/events"

// Host is the seam each runtime implements so the shared core can drive it
// without knowing whether it backs an in-process script or a subprocess plugin.
type Host interface {
	// Kind reports which runtime this host backs.
	Kind() Kind
	// Deliver dispatches an event to the extension identified by id.
	Deliver(id ID, ev events.Event) error
}
```

```go
// internal/extension/registry.go
package extension

import "sync"

// entry is a registered extension plus its host and event-type subscriptions.
type entry struct {
	ext    *Extension
	host   Host
	events map[string]struct{}
}

// deliverable is an enabled extension subscribed to an event type; the Router
// uses it to fan out without holding the registry lock during delivery.
type deliverable struct {
	id   ID
	host Host
}

// Registry is the runtime-agnostic catalog of extensions.
type Registry struct {
	mu      sync.RWMutex
	entries map[ID]*entry
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{entries: make(map[ID]*entry)}
}

// Register adds (or replaces) an extension with its host and subscribed event types.
func (r *Registry) Register(ext *Extension, host Host, eventTypes []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	evs := make(map[string]struct{}, len(eventTypes))
	for _, t := range eventTypes {
		evs[t] = struct{}{}
	}
	r.entries[ext.ID] = &entry{ext: ext, host: host, events: evs}
}

// Get returns the extension by id.
func (r *Registry) Get(id ID) (*Extension, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.entries[id]
	if !ok {
		return nil, false
	}
	return e.ext, true
}

// List returns a snapshot of all registered extensions.
func (r *Registry) List() []*Extension {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Extension, 0, len(r.entries))
	for _, e := range r.entries {
		out = append(out, e.ext)
	}
	return out
}

// SetEnabled toggles an extension's Enabled flag and reconciles Status.
func (r *Registry) SetEnabled(id ID, enabled bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.entries[id]
	if !ok {
		return
	}
	e.ext.Enabled = enabled
	if enabled {
		if e.ext.Status == StatusDisabled {
			e.ext.Status = StatusLoaded
		}
	} else {
		e.ext.Status = StatusDisabled
	}
}

// SetStatus updates an extension's status and error string.
func (r *Registry) SetStatus(id ID, status Status, errMsg string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e, ok := r.entries[id]; ok {
		e.ext.Status = status
		e.ext.Err = errMsg
	}
}

// recipients returns the enabled extensions subscribed to eventType.
func (r *Registry) recipients(eventType string) []deliverable {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []deliverable
	for id, e := range r.entries {
		if !e.ext.Enabled {
			continue
		}
		if _, ok := e.events[eventType]; ok {
			out = append(out, deliverable{id: id, host: e.host})
		}
	}
	return out
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `CGO_ENABLED=1 go test -tags fts5 ./internal/extension/ -v`
Expected: PASS (3 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/extension/extension.go internal/extension/host.go internal/extension/registry.go internal/extension/registry_test.go
git commit -m "feat(extension): identity types, Host seam, and Registry"
```

---

### Task 2: Router — fan bus events out to subscribed extensions via Host

**Files:**
- Create: `internal/extension/router.go`
- Test: `internal/extension/router_test.go`

**Interfaces:**
- Consumes: `Registry`, `Host`, `deliverable` (Task 1); `events.Event`, `events.EventBus`, `events.Subscriber` (`internal/events`).
- Produces:
  - `type Router struct { ... }`; `func NewRouter(reg *Registry) *Router`.
  - `func (rt *Router) OnEvent(ev events.Event)` — implements `events.Subscriber`; delivers to each enabled, subscribed extension via `Host.Deliver`; on a `Deliver` error, calls `reg.SetStatus(id, StatusError, err.Error())`.
  - `func (rt *Router) Attach(bus *events.EventBus)` — subscribes the router to the bus wildcard `"*"`.

- [ ] **Step 1: Write the failing router test**

```go
// internal/extension/router_test.go
package extension

import (
	"errors"
	"sync"
	"testing"

	"github.com/matt0x6f/irc-client/internal/events"
)

// recordingHost records every Deliver call and can be told to fail.
type recordingHost struct {
	mu       sync.Mutex
	got      []events.Event
	gotIDs   []ID
	failWith error
}

func (h *recordingHost) Kind() Kind { return KindScript }
func (h *recordingHost) Deliver(id ID, ev events.Event) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.gotIDs = append(h.gotIDs, id)
	h.got = append(h.got, ev)
	return h.failWith
}

func TestRouterDeliversToSubscribedEnabled(t *testing.T) {
	r := NewRegistry()
	h := &recordingHost{}
	r.Register(&Extension{ID: "a", Enabled: true, Status: StatusLoaded}, h, []string{"privmsg"})

	rt := NewRouter(r)
	rt.OnEvent(events.Event{Type: "privmsg", Data: map[string]interface{}{"text": "hi"}})

	if len(h.got) != 1 || h.gotIDs[0] != "a" || h.got[0].Type != "privmsg" {
		t.Fatalf("deliver = ids:%v evts:%v; want one privmsg to a", h.gotIDs, h.got)
	}
}

func TestRouterSkipsDisabledAndWrongType(t *testing.T) {
	r := NewRegistry()
	h := &recordingHost{}
	r.Register(&Extension{ID: "disabled", Enabled: false, Status: StatusDisabled}, h, []string{"privmsg"})
	r.Register(&Extension{ID: "wrongtype", Enabled: true, Status: StatusLoaded}, h, []string{"join"})

	rt := NewRouter(r)
	rt.OnEvent(events.Event{Type: "privmsg"})

	if len(h.got) != 0 {
		t.Fatalf("expected no deliveries (disabled + wrong-type); got %v", h.gotIDs)
	}
}

func TestRouterDeliverErrorSetsStatus(t *testing.T) {
	r := NewRegistry()
	h := &recordingHost{failWith: errors.New("kaboom")}
	r.Register(&Extension{ID: "a", Enabled: true, Status: StatusLoaded}, h, []string{"privmsg"})

	rt := NewRouter(r)
	rt.OnEvent(events.Event{Type: "privmsg"})

	got, _ := r.Get("a")
	if got.Status != StatusError || got.Err != "kaboom" {
		t.Fatalf("after Deliver error: status=%v err=%q; want error/kaboom", got.Status, got.Err)
	}
}

func TestRouterAttachReceivesBusEvents(t *testing.T) {
	r := NewRegistry()
	h := &recordingHost{}
	r.Register(&Extension{ID: "a", Enabled: true, Status: StatusLoaded}, h, []string{"privmsg"})

	bus := events.NewEventBus()
	rt := NewRouter(r)
	rt.Attach(bus)

	bus.EmitSync(events.Event{Type: "privmsg", Source: events.EventSourceIRC})

	if len(h.got) != 1 || h.gotIDs[0] != "a" {
		t.Fatalf("attached router delivery = %v; want one to a", h.gotIDs)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `CGO_ENABLED=1 go test -tags fts5 ./internal/extension/ -run TestRouter -v`
Expected: FAIL — `NewRouter`/`Router` undefined.

- [ ] **Step 3: Write the Router**

```go
// internal/extension/router.go
package extension

import "github.com/matt0x6f/irc-client/internal/events"

// Router subscribes to the event bus and fans each event out to the enabled
// extensions that subscribed to its type, via their Host. Plugins and scripts
// plug in as Hosts; the Router stays runtime-agnostic.
type Router struct {
	reg *Registry
}

// NewRouter creates a Router backed by the given registry.
func NewRouter(reg *Registry) *Router {
	return &Router{reg: reg}
}

// OnEvent implements events.Subscriber. It delivers ev to every enabled
// extension subscribed to ev.Type; a Deliver error marks that extension errored
// but never blocks delivery to the others.
func (rt *Router) OnEvent(ev events.Event) {
	for _, d := range rt.reg.recipients(ev.Type) {
		if err := d.host.Deliver(d.id, ev); err != nil {
			rt.reg.SetStatus(d.id, StatusError, err.Error())
		}
	}
}

// Attach subscribes the Router to every event on the bus (the "*" wildcard);
// per-type filtering is done from the registry's subscriptions.
func (rt *Router) Attach(bus *events.EventBus) {
	bus.Subscribe("*", rt)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `CGO_ENABLED=1 go test -tags fts5 ./internal/extension/ -run TestRouter -v`
Expected: PASS (4 router tests).

- [ ] **Step 5: Full package + race + gofmt/vet**

Run:
```bash
CGO_ENABLED=1 go test -race -tags fts5 ./internal/extension/ -count=1
gofmt -l internal/extension/
go vet ./internal/extension/
```
Expected: all tests pass under `-race`; `gofmt -l` empty; vet clean.

- [ ] **Step 6: Commit**

```bash
git add internal/extension/router.go internal/extension/router_test.go
git commit -m "feat(extension): event Router fans bus events to subscribed hosts"
```

---

## PR2 Self-Review

- **Spec coverage:** identity types ✓ (Task 1); `Host` seam ✓ (Task 1); Registry register/list/get/enable/status ✓ (Task 1); event Router with per-type + enabled filtering and Deliver-error handling ✓ (Task 2); bus integration via `Attach`/`"*"` ✓ (Task 2). Out of scope (deferred): real script host wiring (PR3), watchdog around Deliver (PR4), command registration + metadata promotion (later).
- **Placeholder scan:** none.
- **Type consistency:** `ID`, `Host`, `deliverable`, `Registry`, `events.Event` used identically across both tasks.

## After PR2

PR3 wires the **real script host** (the PR1 engine) as a `Host` implementing `Deliver` (translating an `events.Event` into a `cascade.TextEvent` and calling `Script.DispatchText`), and adds discovery/lifecycle/hot-reload + the scripts Go module scaffolding. The Deliver call is where PR4 will later wrap the watchdog (deadline + panic recovery).
