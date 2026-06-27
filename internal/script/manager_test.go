package script

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/matt0x6f/irc-client/internal/events"
	"github.com/matt0x6f/irc-client/internal/extension"
)

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
	m := NewManager(bus, dir, testHost(fs.send))
	if err := m.LoadAll(); err != nil {
		t.Fatal(err)
	}

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

	has := func(msg string) bool {
		for _, s := range fs.sent {
			if s.message == msg {
				return true
			}
		}
		return false
	}
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if !has("got-notice") || !has("got-text") || !has("hi dave") || !has("bye dave") {
		t.Fatalf("missing a routed reply; got %+v", fs.sent)
	}
	// the notice must NOT have triggered OnText, and privmsg must NOT have triggered OnNotice:
	// noticer replies only "got-notice"; texter replies only "got-text"; counts must be 1 each.
	cN, cT := 0, 0
	for _, s := range fs.sent {
		if s.message == "got-notice" {
			cN++
		}
		if s.message == "got-text" {
			cT++
		}
	}
	if cN != 1 || cT != 1 {
		t.Fatalf("routing leaked: got-notice=%d got-text=%d (want 1,1)", cN, cT)
	}
}

// noSelf is a selfNick stub that always returns "" (bot has no nick).
func noSelf(int64) string { return "" }

// noResolve is a ResolveNetwork stub that always returns (0, false).
func noResolve(string) (int64, bool) { return 0, false }

// testHost builds a Host with the given sender, noSelf, and noResolve stubs.
// LoadDisabled and PersistEnabled default to nil (nil-safe in Manager).
func testHost(send Sender) Host {
	return Host{Send: send, SelfNick: noSelf, ResolveNetwork: noResolve}
}

// persistHost builds a Host with in-memory persist callbacks for testing
// Enable/Disable persistence.
func persistHost(send Sender, disabled map[string]bool) (Host, *sync.Mutex, *map[string]bool) {
	var mu sync.Mutex
	state := make(map[string]bool)
	// Copy initial disabled set into state.
	for k, v := range disabled {
		state[k] = v
	}
	h := Host{
		Send:           send,
		SelfNick:       noSelf,
		ResolveNetwork: noResolve,
		LoadDisabled: func() map[string]bool {
			mu.Lock()
			defer mu.Unlock()
			out := make(map[string]bool, len(state))
			for k, v := range state {
				out[k] = v
			}
			return out
		},
		PersistEnabled: func(id string, enabled bool) {
			mu.Lock()
			defer mu.Unlock()
			state[id] = !enabled // disabled map: true = disabled
		},
	}
	return h, &mu, &state
}

// fakeSender records Reply sends.
type fakeSender struct {
	mu   sync.Mutex
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
	m := NewManager(bus, "testdata", testHost(fs.send)) // loads each subdir of testdata as a script
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
	m := NewManager(bus, "testdata", testHost(fs.send))
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
	found := false
	for _, s := range fs.sent {
		if s.message == "Hi carol" && s.target == "carol" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected greeter DM reply 'Hi carol' to target 'carol'; got %+v", fs.sent)
	}
}

func TestManagerPanicIsRecoveredAndMarksError(t *testing.T) {
	fs := &fakeSender{}
	bus := events.NewEventBus()
	m := NewManager(bus, "testdata", testHost(fs.send))
	_ = m.LoadAll()

	// The panicker fixture panics in OnText; this must not crash the test process.
	bus.EmitSync(msgEvent(1, "#chan", "dave", "anything", ""))

	ext, ok := m.registry().Get(extension.ID("panicker"))
	if !ok || ext.Status != extension.StatusError {
		t.Fatalf("panicker status = %+v, %v; want StatusError", ext, ok)
	}
}

func TestManagerSkipsServerStatusUser(t *testing.T) {
	fs := &fakeSender{}
	bus := events.NewEventBus()
	m := NewManager(bus, "testdata", testHost(fs.send))
	_ = m.LoadAll()

	// Server/status line: user is "*". No script should be invoked / no reply sent.
	bus.EmitSync(msgEvent(1, "#chan", "*", "!hello", ""))

	fs.mu.Lock()
	defer fs.mu.Unlock()
	if len(fs.sent) != 0 {
		t.Fatalf("expected no replies for user=\"*\" status line; got %+v", fs.sent)
	}
}

func TestManagerReloadPicksUpNewBehavior(t *testing.T) {
	dir := t.TempDir()
	sdir := filepath.Join(dir, "g")
	if err := os.MkdirAll(sdir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(reply string) {
		src := "package main\nimport \"github.com/matt0x6f/irc-client/cascade\"\nfunc OnText(e cascade.TextEvent){ e.Reply(\"" + reply + "\") }\n"
		if err := os.WriteFile(filepath.Join(sdir, "g.go"), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("first")

	fs := &fakeSender{}
	bus := events.NewEventBus()
	m := NewManager(bus, dir, testHost(fs.send))
	if err := m.LoadAll(); err != nil {
		t.Fatal(err)
	}
	bus.EmitSync(msgEvent(1, "#c", "bob", "hi", ""))

	write("second")
	m.reload(sdir)
	bus.EmitSync(msgEvent(1, "#c", "bob", "hi", ""))

	fs.mu.Lock()
	defer fs.mu.Unlock()
	var sawSecond bool
	for _, s := range fs.sent {
		if s.message == "second" {
			sawSecond = true
		}
	}
	if !sawSecond {
		t.Fatalf("after reload expected a 'second' reply; got %+v", fs.sent)
	}
}

func TestManagerUnloadStopsDelivery(t *testing.T) {
	fs := &fakeSender{}
	bus := events.NewEventBus()
	m := NewManager(bus, "testdata", testHost(fs.send))
	_ = m.LoadAll()
	m.unload(extension.ID("greeter"))
	if _, ok := m.registry().Get(extension.ID("greeter")); ok {
		t.Fatalf("greeter should be gone after unload")
	}
	bus.EmitSync(msgEvent(1, "#c", "bob", "!hello", ""))
	fs.mu.Lock()
	defer fs.mu.Unlock()
	for _, s := range fs.sent {
		if s.message == "Hi bob" {
			t.Fatalf("greeter still delivered after unload")
		}
	}
}

func TestScaffoldModuleWritesGoMod(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(events.NewEventBus(), dir, testHost(func(int64, string, string) error { return nil }))
	if err := m.LoadAll(); err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		t.Fatalf("go.mod not written: %v", err)
	}
	s := string(b)
	if !strings.Contains(s, "module cascade-scripts") ||
		!strings.Contains(s, "github.com/matt0x6f/irc-client/cascade "+cascadeSDKVersion) {
		t.Fatalf("go.mod missing module/require:\n%s", s)
	}
}

// reconcile manages only the cascade pin: a user's own module name (and any
// other edits) survive, while the cascade require is brought to the app's SDK.
func TestReconcileModulePreservesUserModuleName(t *testing.T) {
	dir := t.TempDir()
	custom := "module custom\n\ngo 1.21\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}
	m := NewManager(events.NewEventBus(), dir, testHost(func(int64, string, string) error { return nil }))
	_ = m.LoadAll()
	s, _ := os.ReadFile(filepath.Join(dir, "go.mod"))
	if !strings.Contains(string(s), "module custom") {
		t.Fatalf("user module name not preserved:\n%s", s)
	}
	if !strings.Contains(string(s), cascadeModulePath+" "+cascadeSDKVersion) {
		t.Fatalf("cascade pin not reconciled:\n%s", s)
	}
}

func TestWatchStartsAndCloses(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(events.NewEventBus(), dir, testHost(func(int64, string, string) error { return nil }))
	if err := m.LoadAll(); err != nil {
		t.Fatal(err)
	}
	if err := m.Watch(); err != nil {
		t.Fatalf("Watch: %v", err)
	}
	if err := m.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestManagerAutoDisablesRepeatPanicker(t *testing.T) {
	fs := &fakeSender{}
	bus := events.NewEventBus()
	m := NewManager(bus, "testdata", testHost(fs.send))
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

func TestManagerSkipsOwnMessages(t *testing.T) {
	fs := &fakeSender{}
	bus := events.NewEventBus()
	// SelfNick says the bot is "mybot" on network 1.
	m := NewManager(bus, "testdata", Host{
		Send: fs.send,
		SelfNick: func(networkID int64) string {
			if networkID == 1 {
				return "mybot"
			}
			return ""
		},
		ResolveNetwork: noResolve,
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

// TestManagerSetupCallsNetworkSay verifies that a script whose Setup(c) calls
// c.Network("netA").Say("#x", "hello") causes the host's Send to be invoked
// with the resolved network ID, target, and message during LoadAll.
func TestManagerSetupCallsNetworkSay(t *testing.T) {
	fs := &fakeSender{}
	bus := events.NewEventBus()
	m := NewManager(bus, "testdata", Host{
		Send:     fs.send,
		SelfNick: noSelf,
		// "netA" resolves to network ID 42; anything else is unknown.
		ResolveNetwork: func(name string) (int64, bool) {
			if name == "netA" {
				return 42, true
			}
			return 0, false
		},
	})
	if err := m.LoadAll(); err != nil {
		t.Fatal(err)
	}

	fs.mu.Lock()
	defer fs.mu.Unlock()
	for _, s := range fs.sent {
		if s.network == 42 && s.target == "#x" && s.message == "hello" {
			return // pass
		}
	}
	t.Fatalf("expected Send(42, \"#x\", \"hello\") from netsayer Setup; got %+v", fs.sent)
}

// TestDisableStopsTimersAndFiltersEvents checks that Disable:
//  1. stops a c.Every timer (no more ticks after Disable)
//  2. persists (id, false) via PersistEnabled
//  3. filters the script out of event recipients (no delivery after Disable)
func TestDisableStopsTimersAndFiltersEvents(t *testing.T) {
	dir := t.TempDir()
	sdir := filepath.Join(dir, "ticker")
	os.MkdirAll(sdir, 0o755)
	// Script whose timer sends observable messages via c.Network("n").Say so we
	// can verify the timer both fires and stops after Disable.
	src := `package main
import "github.com/matt0x6f/irc-client/cascade"
func Setup(c *cascade.Client) { c.Every("20ms", func() { c.Network("n").Say("#c", "tick") }) }
func OnText(e cascade.TextEvent) { e.Reply("alive") }
`
	os.WriteFile(filepath.Join(sdir, "ticker.go"), []byte(src), 0o644)

	fs := &fakeSender{}
	// Build a host where ResolveNetwork("n") returns a valid ID so timer Say()
	// calls are recorded by fakeSender.
	var mu sync.Mutex
	stateData := make(map[string]bool)
	host := Host{
		Send:     fs.send,
		SelfNick: noSelf,
		ResolveNetwork: func(name string) (int64, bool) {
			if name == "n" {
				return 1, true
			}
			return 0, false
		},
		LoadDisabled: func() map[string]bool {
			mu.Lock()
			defer mu.Unlock()
			out := make(map[string]bool, len(stateData))
			for k, v := range stateData {
				out[k] = v
			}
			return out
		},
		PersistEnabled: func(id string, enabled bool) {
			mu.Lock()
			defer mu.Unlock()
			stateData[id] = !enabled // disabled map: true = disabled
		},
	}
	bus := events.NewEventBus()
	m := NewManager(bus, dir, host)
	if err := m.LoadAll(); err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	id := extension.ID("ticker")

	// Poll until at least one "tick" arrives from the timer so we know it is live.
	countTicks := func() int {
		fs.mu.Lock()
		defer fs.mu.Unlock()
		n := 0
		for _, s := range fs.sent {
			if s.message == "tick" {
				n++
			}
		}
		return n
	}
	if !pollUntil(500*time.Millisecond, func() bool { return countTicks() >= 1 }) {
		t.Fatal("ticker timer never fired before Disable")
	}

	// Disable the script and record how many ticks arrived up to this point.
	m.Disable(id)
	ticksAtDisable := countTicks()

	// Extension must be disabled in registry.
	ext, ok := m.registry().Get(id)
	if !ok || ext.Enabled {
		t.Fatalf("after Disable: Enabled=%v want false", ext.Enabled)
	}

	// PersistEnabled(id, false) must have been called → stateData["ticker"] = true.
	mu.Lock()
	persisted := stateData["ticker"]
	mu.Unlock()
	if !persisted {
		t.Fatal("Disable did not persist disabled state")
	}

	// Wait a settle window and assert no new ticks arrived (timer stopped).
	time.Sleep(100 * time.Millisecond)
	if after := countTicks(); after != ticksAtDisable {
		t.Fatalf("timer still firing after Disable: ticks before=%d after=%d", ticksAtDisable, after)
	}

	// An event sent AFTER Disable must not be delivered.
	bus.EmitSync(msgEvent(1, "#c", "bob", "hello", ""))
	fs.mu.Lock()
	for _, s := range fs.sent {
		if s.message == "alive" {
			fs.mu.Unlock()
			t.Fatal("event delivered to disabled script")
		}
	}
	fs.mu.Unlock()
}

// TestEnableResetsRunawayAndRestoresTimer checks that Enable on a runaway/disabled
// script: clears status → StatusLoaded, clears watchdog strikes, re-runs Setup
// (timer fires again), and persists (id, true).
func TestEnableResetsRunawayAndRestoresTimer(t *testing.T) {
	dir := t.TempDir()
	sdir := filepath.Join(dir, "revive")
	os.MkdirAll(sdir, 0o755)
	// Script with a fast timer and OnText so we can verify delivery after Enable.
	src := `package main
import "github.com/matt0x6f/irc-client/cascade"
func Setup(c *cascade.Client) { c.Every("20ms", func() {}) }
func OnText(e cascade.TextEvent) { e.Reply("revived") }
`
	os.WriteFile(filepath.Join(sdir, "revive.go"), []byte(src), 0o644)

	fs := &fakeSender{}
	bus := events.NewEventBus()
	m := NewManager(bus, dir, Host{
		Send:           fs.send,
		SelfNick:       noSelf,
		ResolveNetwork: noResolve,
		PersistEnabled: func(id string, enabled bool) {}, // no-op spy
	})
	if err := m.LoadAll(); err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	id := extension.ID("revive")

	// Simulate a runaway state via DisableAsRunaway (as the watchdog would).
	m.sched.stopTimers(id)
	m.reg.DisableAsRunaway(id, "test runaway")
	// Inject fake strikes to confirm clearStrikes works.
	m.watchdog.mu.Lock()
	m.watchdog.strikes[id] = 2
	m.watchdog.mu.Unlock()

	ext, _ := m.registry().Get(id)
	if ext.Status != extension.StatusRunaway {
		t.Fatalf("precondition: status=%q want runaway", ext.Status)
	}

	// Enable must reset the script.
	m.Enable(id)

	ext, ok := m.registry().Get(id)
	if !ok || !ext.Enabled || ext.Status != extension.StatusLoaded {
		t.Fatalf("after Enable: Enabled=%v Status=%q, want true/loaded", ext.Enabled, ext.Status)
	}

	// Strikes must be cleared.
	m.watchdog.mu.Lock()
	strikes := m.watchdog.strikes[id]
	m.watchdog.mu.Unlock()
	if strikes != 0 {
		t.Fatalf("strikes after Enable = %d; want 0", strikes)
	}

	// Event delivery must work again after Enable.
	bus.EmitSync(msgEvent(1, "#c", "carol", "ping", ""))
	fs.mu.Lock()
	found := false
	for _, s := range fs.sent {
		if s.message == "revived" {
			found = true
		}
	}
	fs.mu.Unlock()
	if !found {
		t.Fatal("event not delivered after Enable")
	}
}

// TestPersistedDisabledInert checks that a script marked disabled in LoadDisabled
// starts inert after LoadAll (no event delivery, no timer activity).
func TestPersistedDisabledInert(t *testing.T) {
	dir := t.TempDir()
	sdir := filepath.Join(dir, "sleeper")
	os.MkdirAll(sdir, 0o755)
	src := `package main
import "github.com/matt0x6f/irc-client/cascade"
func OnText(e cascade.TextEvent) { e.Reply("wake") }
`
	os.WriteFile(filepath.Join(sdir, "sleeper.go"), []byte(src), 0o644)

	fs := &fakeSender{}
	// sleeper is persisted-disabled.
	host, _, _ := persistHost(fs.send, map[string]bool{"sleeper": true})
	bus := events.NewEventBus()
	m := NewManager(bus, dir, host)
	if err := m.LoadAll(); err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	// Extension must be registered but disabled.
	ext, ok := m.registry().Get(extension.ID("sleeper"))
	if !ok || ext.Enabled {
		t.Fatalf("persisted-disabled script: Enabled=%v want false", ext.Enabled)
	}

	// No events must be delivered.
	bus.EmitSync(msgEvent(1, "#c", "alice", "hi", ""))
	fs.mu.Lock()
	defer fs.mu.Unlock()
	for _, s := range fs.sent {
		if s.message == "wake" {
			t.Fatal("event delivered to persisted-disabled script")
		}
	}
}

// TestReloadByID reloads a script by its ID and picks up new behaviour.
func TestReloadByID(t *testing.T) {
	dir := t.TempDir()
	sdir := filepath.Join(dir, "rscript")
	os.MkdirAll(sdir, 0o755)
	writeV := func(reply string) {
		src := "package main\nimport \"github.com/matt0x6f/irc-client/cascade\"\nfunc OnText(e cascade.TextEvent){ e.Reply(\"" + reply + "\") }\n"
		os.WriteFile(filepath.Join(sdir, "rscript.go"), []byte(src), 0o644)
	}
	writeV("v1")

	fs := &fakeSender{}
	bus := events.NewEventBus()
	m := NewManager(bus, dir, testHost(fs.send))
	if err := m.LoadAll(); err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	bus.EmitSync(msgEvent(1, "#c", "bob", "hi", ""))

	writeV("v2")
	m.ReloadByID(extension.ID("rscript"))
	bus.EmitSync(msgEvent(1, "#c", "bob", "hi", ""))

	fs.mu.Lock()
	defer fs.mu.Unlock()
	sawV2 := false
	for _, s := range fs.sent {
		if s.message == "v2" {
			sawV2 = true
		}
	}
	if !sawV2 {
		t.Fatalf("after ReloadByID expected 'v2' reply; got %+v", fs.sent)
	}
}

// TestNewScriptCreatesAndLoads checks that NewScript writes a file that
// LoadPackage can load and that contains an OnText handler.
func TestNewScriptCreatesAndLoads(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(events.NewEventBus(), dir, testHost(func(int64, string, string) error { return nil }))
	if err := m.LoadAll(); err != nil {
		t.Fatal(err)
	}

	path, err := m.NewScript("myscript")
	if err != nil {
		t.Fatalf("NewScript: %v", err)
	}
	if !strings.HasSuffix(path, filepath.Join("myscript", "myscript.go")) {
		t.Fatalf("unexpected path %q", path)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file not created: %v", err)
	}

	// The generated file must be loadable by LoadPackage and expose OnText.
	s, err := LoadPackage(filepath.Dir(path))
	if err != nil {
		t.Fatalf("LoadPackage on new script: %v", err)
	}
	if !s.Has("OnText") {
		t.Fatal("new script template missing OnText handler")
	}

	// Calling NewScript again with the same name must error.
	if _, err := m.NewScript("myscript"); err == nil {
		t.Fatal("expected error for duplicate script name")
	}
}

// TestNewScriptRejectsUnsafeNames ensures NewScript rejects empty, separator-containing,
// and ".." names.
func TestNewScriptRejectsUnsafeNames(t *testing.T) {
	m := NewManager(events.NewEventBus(), t.TempDir(), testHost(func(int64, string, string) error { return nil }))
	_ = m.LoadAll()

	for _, name := range []string{"", ".", "..", "a/b", "a\\b", "a..b"} {
		if _, err := m.NewScript(name); err == nil {
			t.Errorf("NewScript(%q): expected error", name)
		}
	}
}

// TestSnapshotReturnsList checks that Snapshot returns the registered extensions.
func TestSnapshotReturnsList(t *testing.T) {
	fs := &fakeSender{}
	bus := events.NewEventBus()
	m := NewManager(bus, "testdata", testHost(fs.send))
	if err := m.LoadAll(); err != nil {
		t.Fatal(err)
	}
	snap := m.Snapshot()
	if len(snap) == 0 {
		t.Fatal("Snapshot returned empty slice; expected scripts from testdata")
	}
}

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
