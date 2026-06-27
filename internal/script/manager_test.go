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
	m := NewManager(bus, "testdata", fs.send)
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
	m := NewManager(bus, "testdata", fs.send)
	_ = m.LoadAll()

	// Server/status line: user is "*". No script should be invoked / no reply sent.
	bus.EmitSync(msgEvent(1, "#chan", "*", "!hello", ""))

	fs.mu.Lock()
	defer fs.mu.Unlock()
	if len(fs.sent) != 0 {
		t.Fatalf("expected no replies for user=\"*\" status line; got %+v", fs.sent)
	}
}
