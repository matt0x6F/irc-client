package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/matt0x6f/irc-client/internal/irc"
	"github.com/matt0x6f/irc-client/internal/storage"
)

// newDeepLinkTestApp builds a minimal App backed by a real temp-file Storage,
// mirroring newMarkerTestApp in app_connection_test.go. Shared by the deep-link
// dispatcher tests (Tasks 3 and 4).
func newDeepLinkTestApp(t *testing.T) (*App, *storage.Storage) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "deeplink-test.db")
	s, err := storage.NewStorage(dbPath, 100, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	a := &App{
		storage:              s,
		connectionGapOpen:    make(map[int64]bool),
		reconnectingNetworks: make(map[int64]bool),
		ircClients:           make(map[int64]*irc.IRCClient),
	}
	return a, s
}

// makeNetwork creates a saved network with the given name+address and returns it.
func makeNetwork(t *testing.T, s *storage.Storage, name, address string) *storage.Network {
	t.Helper()
	now := time.Now()
	net := &storage.Network{
		Name: name, Address: address, Port: 6697, Nickname: "me",
		Username: "me", Realname: "Me", CreatedAt: now, UpdatedAt: now,
	}
	if err := s.CreateNetwork(net); err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}
	return net
}

// captureEmits redirects a.emit into a recorder. Returns a *emitRecorder whose
// last(name) returns the most recent payload map for that event, or nil.
type emitRecorder struct {
	events []struct {
		name string
		data map[string]any
	}
}

func (r *emitRecorder) last(name string) map[string]any {
	for i := len(r.events) - 1; i >= 0; i-- {
		if r.events[i].name == name {
			return r.events[i].data
		}
	}
	return nil
}

func captureEmits(a *App) *emitRecorder {
	r := &emitRecorder{}
	a.emitFn = func(name string, data ...any) {
		var m map[string]any
		if len(data) == 1 {
			m, _ = data[0].(map[string]any)
		}
		r.events = append(r.events, struct {
			name string
			data map[string]any
		}{name, m})
	}
	return r
}

func TestHandleDeepLink_UnknownHostEmitsAddNetwork(t *testing.T) {
	a, _ := newDeepLinkTestApp(t)
	rec := captureEmits(a)

	a.handleDeepLink("irc://unknown.example/#chan")

	ev := rec.last("deeplink:add-network")
	if ev == nil {
		t.Fatal("expected deeplink:add-network emit")
	}
	if ev["host"] != "unknown.example" || ev["channel"] != "#chan" {
		t.Fatalf("bad payload: %+v", ev)
	}
}

func TestHandleDeepLink_SingleMatchEmitsJoin(t *testing.T) {
	a, s := newDeepLinkTestApp(t)
	makeNetwork(t, s, "X", "irc.x.net")
	rec := captureEmits(a)

	a.handleDeepLink("irc://irc.x.net/#chan")

	if rec.last("deeplink:join") == nil {
		t.Fatal("expected deeplink:join emit")
	}
}

func TestHandleDeepLink_MultiMatchEmitsDisambiguate(t *testing.T) {
	a, s := newDeepLinkTestApp(t)
	makeNetwork(t, s, "A", "irc.x.net")
	makeNetwork(t, s, "B", "irc.x.net")
	rec := captureEmits(a)

	a.handleDeepLink("irc://irc.x.net/#chan")

	if rec.last("deeplink:disambiguate") == nil {
		t.Fatal("expected deeplink:disambiguate emit")
	}
}

func TestProcessStartupArgs_PicksUpDeepLink(t *testing.T) {
	a, _ := newDeepLinkTestApp(t)
	rec := captureEmits(a)
	a.processStartupArgs([]string{"/path/to/cascade", "irc://unknown.example/#x"})
	if rec.last("deeplink:add-network") == nil {
		t.Fatal("expected deep link to be handled from args")
	}
}

func TestHandleDeepLink_UnknownHostStoresPrefill(t *testing.T) {
	a, _ := newDeepLinkTestApp(t)
	_ = captureEmits(a)

	a.handleDeepLink("ircs://unknown.example:7000/#chan")

	p := a.GetPendingNetworkPrefill()
	if p == nil {
		t.Fatal("expected pending prefill stored")
	}
	if p.Host != "unknown.example" || p.Port != 7000 || !p.TLS || p.Channel != "#chan" {
		t.Fatalf("bad prefill: %+v", p)
	}
	// Getter clears: second call returns nil.
	if a.GetPendingNetworkPrefill() != nil {
		t.Fatal("expected prefill to be cleared after read")
	}
}

func TestConnectSavedNetwork_UnknownIDErrors(t *testing.T) {
	a, _ := newDeepLinkTestApp(t)
	if err := a.ConnectSavedNetwork(999999); err == nil {
		t.Fatal("expected error for unknown network id")
	}
}

func TestHandleDeepLink_BuffersUntilFrontendReady(t *testing.T) {
	a, _ := newDeepLinkTestApp(t)
	_ = captureEmits(a)
	a.handleDeepLink("irc://unknown.example/#c")

	p := a.DrainPendingDeepLink()
	if p == nil || p.Event != "deeplink:add-network" {
		t.Fatalf("expected buffered add-network, got %+v", p)
	}
	if a.DrainPendingDeepLink() != nil {
		t.Fatal("expected pending cleared after drain")
	}
	// After drain, frontend is ready: a new link is not buffered.
	a.handleDeepLink("irc://unknown.example/#d")
	if a.DrainPendingDeepLink() != nil {
		t.Fatal("expected no buffering once frontend ready")
	}
}

func TestFindNetworksByAddress_MultipleMatches(t *testing.T) {
	a, s := newDeepLinkTestApp(t)
	makeNetwork(t, s, "Libera work", "irc.libera.chat")
	makeNetwork(t, s, "Libera personal", "irc.libera.chat")
	makeNetwork(t, s, "OFTC", "irc.oftc.net")

	ids := a.findNetworksByAddress("irc.libera.chat")
	if len(ids) != 2 {
		t.Fatalf("want 2 matches, got %d (%v)", len(ids), ids)
	}
}
