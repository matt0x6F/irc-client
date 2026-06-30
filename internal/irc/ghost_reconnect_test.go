package irc

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/ergochat/irc-go/ircmsg"
	"github.com/matt0x6f/irc-client/internal/events"
	"github.com/matt0x6f/irc-client/internal/storage"
)

// newConnectTestClient builds an IRCClient with the dependencies the connect
// handler touches: a live (pipe-backed) library connection, storage, a network,
// and an event bus.
func newConnectTestClient(t *testing.T) *IRCClient {
	t.Helper()
	dir := t.TempDir()
	s, err := storage.NewStorage(filepath.Join(dir, "test.db"), 100, 20*time.Millisecond)
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	net := &storage.Network{Name: "ErgoIRC", Address: "irc.ergo.chat", Nickname: "matt0x6f", CreatedAt: time.Now()}
	if err := s.CreateNetwork(net); err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}

	return &IRCClient{
		conn:      newQuitAwarePipe(t),
		eventBus:  events.NewEventBus(),
		storage:   s,
		networkID: net.ID,
		network:   net,
	}
}

// TestConnectAfterQuitDoesNotResurrectClient is the regression guard for ghost
// resurrection. After we signal quit on a client (a deliberate teardown — e.g.
// the stale-client cleanup that runs before an app-driven reconnect), the
// ergochat Loop can still fire ONE final connect callback: it does not re-check
// its quit flag after the reconnect delay, so a Quit() that lands mid-cycle is
// honored one reconnect too late. That doomed connect must be a no-op. Otherwise
// the ghost re-registers (stealing the nick), reports itself connected via
// IsConnected()/GetConnectionStatus, and spams "Disconnected from server" into
// the surviving session's window — the exact symptom observed in the field.
func TestConnectAfterQuitDoesNotResurrectClient(t *testing.T) {
	c := newConnectTestClient(t)

	c.signalQuit("Disconnecting")

	// The library Loop's final reconnect: Connect() succeeds and invokes the
	// registered connect handler even though we already asked to quit.
	c.onConnect(ircmsg.Message{})

	if c.IsConnected() {
		t.Fatal("connect callback after signalQuit resurrected the client — ghost session: IsConnected()/GetConnectionStatus will report a doomed socket as connected")
	}
}

// TestConnectOnLiveClientMarksConnected is the positive control: the abandon
// guard must not break a normal connect. A client that has NOT been told to quit
// must still come up connected when its connect handler fires.
func TestConnectOnLiveClientMarksConnected(t *testing.T) {
	c := newConnectTestClient(t)

	c.onConnect(ircmsg.Message{})

	if !c.IsConnected() {
		t.Fatal("connect callback on a live client did not mark it connected — the abandon guard is too broad")
	}
}

// TestDropAbandonsClientSoLibraryReconnectIsIgnored closes the race the field
// bug actually hit. On a drop the ergochat Loop reconnects on its own (immediately
// for a long-lived connection), firing the connect handler again — but the app is
// the sole reconnect owner and always reconnects through a FRESH IRCClient. So a
// dropped client must be abandoned the instant it drops (the disconnect callback
// completes before the Loop's reconnect), making any library-driven reconnect on
// the same object a no-op. Without this the ghost re-registers and steals the nick.
func TestDropAbandonsClientSoLibraryReconnectIsIgnored(t *testing.T) {
	c := newConnectTestClient(t)

	// Client comes up.
	c.onConnect(ircmsg.Message{})
	if !c.IsConnected() {
		t.Fatal("precondition: client should be connected after onConnect")
	}

	// Unexpected drop: the library fires the disconnect callback (which, in the
	// real Loop, runs to completion before any reconnect).
	c.onDisconnect(ircmsg.Message{})
	if c.IsConnected() {
		t.Fatal("client still reports connected after a drop")
	}

	// The library Loop's own reconnect fires the connect handler again on the same
	// (now-orphaned) client. It must be ignored — the app owns reconnection.
	c.onConnect(ircmsg.Message{})
	if c.IsConnected() {
		t.Fatal("library auto-reconnect resurrected a dropped client — ghost session holding the nick while the app reconnects a fresh client")
	}
}
