# Cascade Scripting v1 — PR7: More event types (notice · join · part) + messageType split

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Let scripts react to more than channel text. Add `OnNotice`, `OnJoin`, `OnPart` handlers, and add a `messageType` discriminator to the inbound-message event so NOTICEs route to `OnNotice` (not `OnText`) — fixing the long-flagged gap.

**Architecture:** The producer (`internal/irc`) tags each `message.received` event with `messageType` (`"privmsg"`/`"notice"`). The cascade SDK gains `NoticeEvent`/`JoinEvent`/`PartEvent`; the engine gains `DispatchNotice`/`DispatchJoin`/`DispatchPart`; the Manager subscribes a script to the right bus events by which handlers it defines, and `Deliver` routes by event type + `messageType`. Reply on each event routes to its origin via the existing per-network sender. Write-side actions (`Say` to arbitrary targets, bot `Join`/`Part`, timers) need a network-addressing model and are deferred to PR8.

**Scope decision — what this PR is NOT:** no `c.Say`/`c.Join`/`c.Part`/timers and no `c.Network(...)` handle. Those require a coherent "how scripts address networks for non-event actions" design, which is its own focused PR (PR8). PR7 stays on the **read side** (events) + each event's origin-routed `Reply`, reusing the established pattern. This is decomposition, not deferral of anything unresolved here.

**Tech Stack:** Go; the PR4–6 manager; the cascade SDK module; the event bus.

## Global Constraints

- **Module path lowercase** `github.com/matt0x6f/irc-client`; cascade SDK `github.com/matt0x6f/irc-client/cascade`.
- **Tests:** `CGO_ENABLED=1 go test -tags fts5 <pkg> -count=1`; `internal/script` concurrency-bearing → also `-race`. cascade module tested from its dir. gofmt-clean; `go vet`.
- **cascade is pre-release** (no `v1.0.0` tag yet) → expanding the cascade API is fine; finalize the v1 surface before the tag.
- **Backward-safe routing:** the Manager treats `messageType == "notice"` → `OnNotice`; anything else (`"privmsg"`, `"action"`, or **absent**) → `OnText`. So a missed producer site can never drop a privmsg; it can only (at worst) leave the pre-existing notice-as-text behavior — but Task 1 tags the notice sites, which is what matters.

## Grounded facts (from internal/irc)

- Inbound messages (privmsg AND notice) emit `EventMessageReceived = "message.received"` with Data `{network, networkId(int64), channel, user, message, pmTarget}` — **no type field today** (the bug). Privmsg emits: ~`client.go:1370` (+ status lines 969/998, `user:"*"`). Notice emits: ~`client.go:4548`, `4603`.
- `EventUserJoined = "user.joined"` Data `{network, networkId, channel, user}`.
- `EventUserParted = "user.parted"` Data `{network, networkId, channel, user, reason}`.
- `internal/irc` imports neither `internal/script` nor `internal/extension` → a `script` test may import `irc` for a contract guard with no cycle.

---

## PR7 — File Structure

- `cascade/events.go` + `events_test.go` — `NoticeEvent`/`JoinEvent`/`PartEvent` (Task 1).
- `internal/irc/client.go` — add `"messageType"` to `EventMessageReceived` emits (Task 2).
- `internal/script/engine.go` — `DispatchNotice`/`DispatchJoin`/`DispatchPart` (Task 3).
- `internal/script/manager.go` — event-type consts, subscription, `Deliver` routing, `buildNotice/Join/PartEvent` (Task 3).
- `internal/script/manager_test.go` + a new `internal/script/contract_test.go` — routing tests + irc-constant guard (Task 3).

---

## Task 1: cascade SDK — NoticeEvent, JoinEvent, PartEvent

**Files:**
- Modify: `cascade/events.go`, `cascade/events_test.go`

**Interfaces:**
- Produces (host-constructed; scripts read fields + `Reply`):
  - `NoticeEvent{ Nick, Channel, Message string }`; `Reply(string)`; `IsDM() bool`; `NewNoticeEvent(nick, channel, message string, reply func(string)) NoticeEvent`.
  - `JoinEvent{ Nick, Channel string }`; `Reply(string)` (to the channel); `NewJoinEvent(nick, channel string, reply func(string)) JoinEvent`.
  - `PartEvent{ Nick, Channel, Reason string }`; `Reply(string)` (to the channel); `NewPartEvent(nick, channel, reason string, reply func(string)) PartEvent`.

- [ ] **Step 1: Write the failing test (cascade module)**

```go
// add to cascade/events_test.go
func TestNoticeJoinPartEvents(t *testing.T) {
	var got []string
	sink := func(s string) { got = append(got, s) }

	n := NewNoticeEvent("serv", "#c", "hi", sink)
	if n.Nick != "serv" || n.Channel != "#c" || n.Message != "hi" || n.IsDM() {
		t.Fatalf("notice fields wrong: %+v", n)
	}
	n.Reply("ack")

	j := NewJoinEvent("bob", "#c", sink)
	if j.Nick != "bob" || j.Channel != "#c" {
		t.Fatalf("join fields wrong: %+v", j)
	}
	j.Reply("welcome bob")

	p := NewPartEvent("bob", "#c", "bye", sink)
	if p.Nick != "bob" || p.Channel != "#c" || p.Reason != "bye" {
		t.Fatalf("part fields wrong: %+v", p)
	}
	p.Reply("cya")

	if len(got) != 3 || got[0] != "ack" || got[1] != "welcome bob" || got[2] != "cya" {
		t.Fatalf("replies = %v", got)
	}
}
```

- [ ] **Step 2: Run — fails (types undefined)**

Run: `(cd cascade && CGO_ENABLED=1 go test ./... -run TestNoticeJoinPartEvents -v)`
Expected: FAIL.

- [ ] **Step 3: Implement in `cascade/events.go`**

```go
// NoticeEvent is delivered to OnNotice handlers (an inbound NOTICE).
type NoticeEvent struct {
	Nick    string
	Channel string
	Message string

	replyFn func(string)
}

func NewNoticeEvent(nick, channel, message string, reply func(string)) NoticeEvent {
	return NoticeEvent{Nick: nick, Channel: channel, Message: message, replyFn: reply}
}
func (e NoticeEvent) IsDM() bool { return e.Channel == "" || (e.Channel[0] != '#' && e.Channel[0] != '&') }
func (e NoticeEvent) Reply(msg string) {
	if e.replyFn != nil {
		e.replyFn(msg)
	}
}

// JoinEvent is delivered to OnJoin handlers when a user joins a channel.
type JoinEvent struct {
	Nick    string
	Channel string

	replyFn func(string)
}

func NewJoinEvent(nick, channel string, reply func(string)) JoinEvent {
	return JoinEvent{Nick: nick, Channel: channel, replyFn: reply}
}
func (e JoinEvent) Reply(msg string) {
	if e.replyFn != nil {
		e.replyFn(msg)
	}
}

// PartEvent is delivered to OnPart handlers when a user leaves a channel.
type PartEvent struct {
	Nick    string
	Channel string
	Reason  string

	replyFn func(string)
}

func NewPartEvent(nick, channel, reason string, reply func(string)) PartEvent {
	return PartEvent{Nick: nick, Channel: channel, Reason: reason, replyFn: reply}
}
func (e PartEvent) Reply(msg string) {
	if e.replyFn != nil {
		e.replyFn(msg)
	}
}
```

- [ ] **Step 4: Green + commit**

Run: `(cd cascade && CGO_ENABLED=1 go test ./... -count=1)`
```bash
git add cascade/
git commit -m "feat(cascade): add NoticeEvent, JoinEvent, PartEvent"
```

---

## Task 2: Producer — tag `message.received` with `messageType`

**Files:**
- Modify: `internal/irc/client.go` (every `EventMessageReceived` emit)

**Interfaces:**
- Each `EventMessageReceived` Data gains `"messageType": <string>` — `"privmsg"` for PRIVMSG emits, `"notice"` for NOTICE emits. (Use the message type already computed/known at each site; a nearby `storage.Message` literal carries the same value in its `MessageType` field.)

- [ ] **Step 1: Find every emit site**

Run: `grep -n "Type: EventMessageReceived" internal/irc/client.go`
Expected: ~5 sites (PRIVMSG, server/status `user:"*"`, NOTICE ×2). For EACH, the `Data` map currently has `network/networkId/channel/user/message` (and sometimes `pmTarget`).

- [ ] **Step 2: Add `messageType` to each Data map**

At each `EventMessageReceived` emit, add a `"messageType"` key with the correct value for that site:
- PRIVMSG sites (e.g. ~`client.go:1370`) → `"messageType": "privmsg"`.
- NOTICE sites (~`client.go:4548`, `4603`) → `"messageType": "notice"`.
- The `user:"*"` server/status sites (~969/998): use the message type already in scope there (it is privmsg-class; `"privmsg"` is correct, and these are skipped downstream by the `nick=="*"` guard regardless).

Do NOT change any other field. Keep `storage.Message{...MessageType: ...}` literals as they are — you are only adding the key to the *event* `Data` map.

- [ ] **Step 3: Verify the package still builds + tests pass**

Run:
```bash
grep -c '"messageType"' internal/irc/client.go     # should equal the number of EventMessageReceived emits
CGO_ENABLED=1 go test -tags fts5 ./internal/irc/ -count=1
gofmt -l internal/irc/
```
Expected: every emit tagged; irc tests green; gofmt empty. (Behavioral routing is exercised by Task 3's Manager tests.)

- [ ] **Step 4: Commit**

```bash
git add internal/irc/client.go
git commit -m "feat(irc): tag message.received events with messageType (privmsg/notice)"
```

---

## Task 3: Engine dispatch + Manager routing for notice/join/part

**Files:**
- Modify: `internal/script/engine.go` (`DispatchNotice`/`DispatchJoin`/`DispatchPart`)
- Modify: `internal/script/manager.go` (consts, subscription, `Deliver` routing, builders)
- Modify: `internal/script/manager_test.go` (routing tests)
- Create: `internal/script/contract_test.go` (irc-constant guard)

**Interfaces:**
- Consumes: `cascade.{NoticeEvent,JoinEvent,PartEvent}` (Task 1); the `messageType` Data key (Task 2); `irc.{EventUserJoined,EventUserParted}` (test-only import).
- Produces: `Script.DispatchNotice/DispatchJoin/DispatchPart`; manager consts `eventUserJoined="user.joined"`, `eventUserParted="user.parted"`; routing in `Deliver`; `buildNoticeEvent/buildJoinEvent/buildPartEvent`.

- [ ] **Step 1: Engine dispatch methods (mirror DispatchText)**

In `internal/script/engine.go`, add (same shape as `DispatchText`, using the `handlers` map + reflect):
```go
func (s *Script) DispatchNotice(e cascade.NoticeEvent) { s.call("OnNotice", e) }
func (s *Script) DispatchJoin(e cascade.JoinEvent)     { s.call("OnJoin", e) }
func (s *Script) DispatchPart(e cascade.PartEvent)     { s.call("OnPart", e) }
```
If `DispatchText` does the reflect call inline rather than via a helper, factor a small `call(name string, arg any)` helper that looks up `s.handlers[name]` and `fn.Call([]reflect.Value{reflect.ValueOf(arg)})` (guard missing handler), and route `DispatchText` through it too — keep behavior identical.

- [ ] **Step 2: Failing Manager routing tests**

```go
// add to internal/script/manager_test.go — fixtures: create temp scripts with OnNotice/OnJoin/OnPart.
func TestManagerRoutesNoticeJoinPart(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		d := filepath.Join(dir, name)
		os.MkdirAll(d, 0o755)
		src := "package main\nimport \"github.com/matt0x6f/irc-client/cascade\"\n" + body
		os.WriteFile(filepath.Join(d, "s.go"), []byte(src), 0o644)
	}
	write("noticer", `func OnNotice(e cascade.NoticeEvent){ e.Reply("got-notice") }`)
	write("joiner", `func OnJoin(e cascade.JoinEvent){ e.Reply("hi "+e.Nick) }`)
	write("parter", `func OnPart(e cascade.PartEvent){ e.Reply("bye "+e.Nick) }`)
	write("texter", `func OnText(e cascade.TextEvent){ e.Reply("got-text") }`)

	fs := &fakeSender{}
	bus := events.NewEventBus()
	m := NewManager(bus, dir, fs.send, noSelf)
	if err := m.LoadAll(); err != nil { t.Fatal(err) }

	// a NOTICE must reach OnNotice, NOT OnText
	bus.EmitSync(events.Event{Type: "message.received", Data: map[string]interface{}{
		"networkId": int64(1), "channel": "#c", "user": "bob", "message": "x", "messageType": "notice"}})
	// a PRIVMSG must reach OnText, NOT OnNotice
	bus.EmitSync(events.Event{Type: "message.received", Data: map[string]interface{}{
		"networkId": int64(1), "channel": "#c", "user": "bob", "message": "x", "messageType": "privmsg"}})
	// a JOIN / PART
	bus.EmitSync(events.Event{Type: "user.joined", Data: map[string]interface{}{
		"networkId": int64(1), "channel": "#c", "user": "dave"}})
	bus.EmitSync(events.Event{Type: "user.parted", Data: map[string]interface{}{
		"networkId": int64(1), "channel": "#c", "user": "dave", "reason": "z"}})

	has := func(msg string) bool { for _, s := range fs.sent { if s.message == msg { return true } }; return false }
	fs.mu.Lock(); defer fs.mu.Unlock()
	if !has("got-notice") || !has("got-text") || !has("hi dave") || !has("bye dave") {
		t.Fatalf("missing a routed reply; got %+v", fs.sent)
	}
	if has("got-text") == has("got-notice") { /* both true is fine */ }
	// the notice must NOT have triggered OnText, and privmsg must NOT have triggered OnNotice:
	// noticer replies only "got-notice"; texter replies only "got-text"; counts must be 1 each.
	cN, cT := 0, 0
	for _, s := range fs.sent { if s.message == "got-notice" { cN++ }; if s.message == "got-text" { cT++ } }
	if cN != 1 || cT != 1 {
		t.Fatalf("routing leaked: got-notice=%d got-text=%d (want 1,1)", cN, cT)
	}
}
```

```go
// internal/script/contract_test.go — guard the hardcoded event strings against the irc constants.
package script

import (
	"testing"

	"github.com/matt0x6f/irc-client/internal/irc"
)

func TestEventStringContract(t *testing.T) {
	if eventMessageReceived != irc.EventMessageReceived {
		t.Fatalf("eventMessageReceived %q != irc.EventMessageReceived %q", eventMessageReceived, irc.EventMessageReceived)
	}
	if eventUserJoined != irc.EventUserJoined {
		t.Fatalf("eventUserJoined %q != irc.EventUserJoined %q", eventUserJoined, irc.EventUserJoined)
	}
	if eventUserParted != irc.EventUserParted {
		t.Fatalf("eventUserParted %q != irc.EventUserParted %q", eventUserParted, irc.EventUserParted)
	}
}
```

- [ ] **Step 3: Run — fails (consts/routing/dispatch undefined)**

Run: `CGO_ENABLED=1 go test -tags fts5 ./internal/script/ -run "TestManagerRoutesNoticeJoinPart|TestEventStringContract" -v`
Expected: FAIL.

- [ ] **Step 4: Implement subscription + routing in `manager.go`**

Add consts:
```go
const (
	eventUserJoined = "user.joined"
	eventUserParted = "user.parted"
)
```
In `loadDir`, build the subscription set from the handlers the script defines:
```go
	var evs []string
	if s.Has("OnText") || s.Has("OnNotice") {
		evs = append(evs, eventMessageReceived)
	}
	if s.Has("OnJoin") {
		evs = append(evs, eventUserJoined)
	}
	if s.Has("OnPart") {
		evs = append(evs, eventUserParted)
	}
```
In `dispatch` (the watchdog-wrapped body), extend the switch:
```go
	switch ev.Type {
	case eventMessageReceived:
		mt, _ := ev.Data["messageType"].(string)
		if mt == "notice" {
			if ne, ok := m.buildNoticeEvent(ev); ok {
				l.mu.Lock(); defer l.mu.Unlock(); l.script.DispatchNotice(ne)
			}
			return nil
		}
		if te, ok := m.buildTextEvent(ev); ok { // privmsg / action / absent
			l.mu.Lock(); defer l.mu.Unlock(); l.script.DispatchText(te)
		}
	case eventUserJoined:
		if je, ok := m.buildJoinEvent(ev); ok {
			l.mu.Lock(); defer l.mu.Unlock(); l.script.DispatchJoin(je)
		}
	case eventUserParted:
		if pe, ok := m.buildPartEvent(ev); ok {
			l.mu.Lock(); defer l.mu.Unlock(); l.script.DispatchPart(pe)
		}
	}
	return nil
```
Note: a script subscribed to `message.received` for `OnText` only will still get notice events delivered here, but `DispatchNotice` no-ops if it has no `OnNotice` handler (the reflect lookup misses) — so routing is correct even when a script defines only one of the two. (Confirm `DispatchNotice`/`DispatchText` are no-ops when the handler is absent — they are, via the `handlers` map guard.)

Add the builders mirroring `buildTextEvent` (same `selfNick`/`"*"`/empty guards, reply target = channel or sender):
```go
func (m *Manager) buildNoticeEvent(ev events.Event) (cascade.NoticeEvent, bool) {
	nick, channel, message, networkID, ok := m.msgFields(ev)
	if !ok { return cascade.NoticeEvent{}, false }
	reply := m.replyTo(networkID, channel, nick)
	return cascade.NewNoticeEvent(nick, channel, message, reply), true
}
func (m *Manager) buildJoinEvent(ev events.Event) (cascade.JoinEvent, bool) {
	nick, _ := ev.Data["user"].(string)
	channel, _ := ev.Data["channel"].(string)
	networkID, _ := ev.Data["networkId"].(int64)
	if nick == "" || nick == "*" || (m.selfNick != nil && nick == m.selfNick(networkID)) {
		return cascade.JoinEvent{}, false
	}
	return cascade.NewJoinEvent(nick, channel, m.replyTo(networkID, channel, nick)), true
}
func (m *Manager) buildPartEvent(ev events.Event) (cascade.PartEvent, bool) {
	nick, _ := ev.Data["user"].(string)
	channel, _ := ev.Data["channel"].(string)
	reason, _ := ev.Data["reason"].(string)
	networkID, _ := ev.Data["networkId"].(int64)
	if nick == "" || nick == "*" || (m.selfNick != nil && nick == m.selfNick(networkID)) {
		return cascade.PartEvent{}, false
	}
	return cascade.NewPartEvent(nick, channel, reason, m.replyTo(networkID, channel, nick)), true
}
```
Factor the shared bits out of the existing `buildTextEvent` so it reuses them (DRY): a `replyTo(networkID int64, channel, nick string) func(string)` helper (the channel-or-sender routing + sender closure) and, if convenient, a `msgFields(ev) (nick, channel, message string, networkID int64, ok bool)` helper applying the `""`/`"*"`/selfNick guards. Refactor `buildTextEvent` to use them so behavior is identical (its existing tests must still pass).

- [ ] **Step 5: Green (incl -race) + full suite + commit**

Run:
```bash
CGO_ENABLED=1 go test -race -tags fts5 ./internal/script/ -count=1
CGO_ENABLED=1 go test -tags fts5 ./internal/... -count=1
gofmt -l internal/script/
```
Expected: routing + contract tests pass; the notice/privmsg leak check passes (each fires exactly its own handler); existing Manager tests unaffected; full suite green; gofmt empty.
```bash
git add internal/script/
git commit -m "feat(script): route notice/join/part to OnNotice/OnJoin/OnPart"
```

---

## PR7 Self-Review

- **Spec coverage:** `messageType` discriminator ✓ (T2); `OnNotice` (notice no longer fires `OnText`) ✓ (T3 leak check); `OnJoin`/`OnPart` ✓ (T3); each event origin-routed `Reply` ✓ (T1/T3); contract guard against irc constants ✓ (T3).
- **Backward-safe:** absent `messageType` → `OnText` (no privmsg regression); self/`"*"` guards reused for all event types.
- **DRY:** notice/join/part builders reuse `replyTo`/`msgFields` factored from `buildTextEvent`.
- **Type consistency:** `eventUserJoined`/`eventUserParted` == irc constants (guarded); cascade `New*Event` signatures match the builders.

## After PR7 — remaining deferred work

| Deferred | Where |
|----------|-------|
| **Write-side actions + network model:** `c.Say(target,msg)`, bot `c.Join`/`c.Part`, `ReplyNotice`/`Action`, and the `c.Network(...)`/timer (`Every`/`After`) design | **PR8 (actions & timers)** |
| **Frontend management & permissions surface** + enable/disable persistence + watchdog re-enable (reset `StatusRunaway` + strike count) | **PR9 (frontend)** |
| **OnAction** (CTCP ACTION / `/me`) handler | **PR8 or later** |
| **Permission enforcement** + harden `parseManifest` to the leading comment block | **v2** |
| **Cut the `cascade/v1.0.0` tag** | **Release** |
