package irc

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/matt0x6f/irc-client/internal/events"
	"github.com/matt0x6f/irc-client/internal/storage"
)

// newRejoinTestClient builds a real IRCClient backed by temp-file storage with a
// network nicknamed "matt0x6f" for exercising channel-rejoin selection.
func newRejoinTestClient(t *testing.T) *IRCClient {
	t.Helper()
	dir := t.TempDir()
	s, err := storage.NewStorage(filepath.Join(dir, "test.db"), 100, 20*time.Millisecond)
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	net := &storage.Network{Name: "Ergo", Address: "irc.example.com", Nickname: "matt0x6f", CreatedAt: time.Now()}
	if err := s.CreateNetwork(net); err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}
	return &IRCClient{
		eventBus:  events.NewEventBus(),
		storage:   s,
		networkID: net.ID,
		network:   net,
	}
}

func mustCreateChannel(t *testing.T, s *storage.Storage, ch *storage.Channel) {
	t.Helper()
	if err := s.CreateChannel(ch); err != nil {
		t.Fatalf("CreateChannel(%s): %v", ch.Name, err)
	}
}

func channelNameSet(chans []storage.Channel) map[string]bool {
	out := make(map[string]bool, len(chans))
	for _, c := range chans {
		out[c.Name] = true
	}
	return out
}

// TestChannelsToJoinReconnectIncludesOpenNonAutoJoin is the regression test for the
// sleep/wake bug: after an unexpected disconnect we must rejoin every channel we
// were in (is_open OR still a member), not just the startup auto-join set. Without
// this, the server never resends NAMES for those channels and their nick lists go
// stale — exactly "most channels did not refresh their nickname lists".
func TestChannelsToJoinReconnectIncludesOpenNonAutoJoin(t *testing.T) {
	c := newRejoinTestClient(t)
	now := time.Now()

	// open but NOT auto-join — the exact channel that failed to refresh after wake
	mustCreateChannel(t, c.storage, &storage.Channel{NetworkID: c.networkID, Name: "#alpha", IsOpen: true, AutoJoin: false, CreatedAt: now})
	// auto-join but currently closed and not a member
	mustCreateChannel(t, c.storage, &storage.Channel{NetworkID: c.networkID, Name: "#bravo", IsOpen: false, AutoJoin: true, CreatedAt: now})
	// both open and auto-join
	mustCreateChannel(t, c.storage, &storage.Channel{NetworkID: c.networkID, Name: "#charlie", IsOpen: true, AutoJoin: true, CreatedAt: now})
	// neither open nor auto-join — must never be rejoined
	mustCreateChannel(t, c.storage, &storage.Channel{NetworkID: c.networkID, Name: "#delta", IsOpen: false, AutoJoin: false, CreatedAt: now})
	// closed but we're still a member — counts as a channel we were in
	mustCreateChannel(t, c.storage, &storage.Channel{NetworkID: c.networkID, Name: "#echo", IsOpen: false, AutoJoin: false, CreatedAt: now})
	echo, err := c.storage.GetChannelByName(c.networkID, "#echo")
	if err != nil {
		t.Fatalf("GetChannelByName(#echo): %v", err)
	}
	if err := c.storage.AddChannelUser(echo.ID, "matt0x6f", ""); err != nil {
		t.Fatalf("AddChannelUser: %v", err)
	}

	got, err := c.channelsToJoin(true)
	if err != nil {
		t.Fatalf("channelsToJoin(reconnect=true): %v", err)
	}
	set := channelNameSet(got)

	for _, want := range []string{"#alpha", "#charlie", "#echo"} {
		if !set[want] {
			t.Errorf("reconnect rejoin set missing %s; got %v", want, set)
		}
	}
	for _, notWant := range []string{"#bravo", "#delta"} {
		if set[notWant] {
			t.Errorf("reconnect rejoin set should not include %s; got %v", notWant, set)
		}
	}
}

// TestChannelsToJoinStartupUsesAutoJoinOnly asserts a fresh startup connect keeps
// the original behavior: only channels with auto_join enabled are joined, so an
// open-but-not-auto-join channel is NOT silently rejoined on every app launch.
func TestChannelsToJoinStartupUsesAutoJoinOnly(t *testing.T) {
	c := newRejoinTestClient(t)
	now := time.Now()

	mustCreateChannel(t, c.storage, &storage.Channel{NetworkID: c.networkID, Name: "#alpha", IsOpen: true, AutoJoin: false, CreatedAt: now})
	mustCreateChannel(t, c.storage, &storage.Channel{NetworkID: c.networkID, Name: "#bravo", IsOpen: false, AutoJoin: true, CreatedAt: now})
	mustCreateChannel(t, c.storage, &storage.Channel{NetworkID: c.networkID, Name: "#charlie", IsOpen: true, AutoJoin: true, CreatedAt: now})

	got, err := c.channelsToJoin(false)
	if err != nil {
		t.Fatalf("channelsToJoin(reconnect=false): %v", err)
	}
	set := channelNameSet(got)

	for _, want := range []string{"#bravo", "#charlie"} {
		if !set[want] {
			t.Errorf("startup join set missing %s; got %v", want, set)
		}
	}
	if set["#alpha"] {
		t.Errorf("startup join set should not include open-but-not-auto-join #alpha; got %v", set)
	}
}
