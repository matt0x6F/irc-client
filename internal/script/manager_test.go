package script

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

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
	m := NewManager(bus, dir, fs.send, noSelf)
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
	m := NewManager(bus, "testdata", fs.send, noSelf) // loads each subdir of testdata as a script
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
	m := NewManager(bus, "testdata", fs.send, noSelf)
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
	m := NewManager(bus, "testdata", fs.send, noSelf)
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
	m := NewManager(bus, "testdata", fs.send, noSelf)
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
	m := NewManager(bus, dir, fs.send, noSelf)
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
	m := NewManager(bus, "testdata", fs.send, noSelf)
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
	m := NewManager(events.NewEventBus(), dir, func(int64, string, string) error { return nil }, noSelf)
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

func TestScaffoldModuleDoesNotOverwrite(t *testing.T) {
	dir := t.TempDir()
	custom := "module custom\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}
	m := NewManager(events.NewEventBus(), dir, func(int64, string, string) error { return nil }, noSelf)
	_ = m.LoadAll()
	b, _ := os.ReadFile(filepath.Join(dir, "go.mod"))
	if string(b) != custom {
		t.Fatalf("existing go.mod was overwritten: %s", b)
	}
}

func TestWatchStartsAndCloses(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(events.NewEventBus(), dir, func(int64, string, string) error { return nil }, noSelf)
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
