package irc

import (
	"testing"
	"time"

	"github.com/matt0x6f/irc-client/internal/storage"
)

// The pending join key is recorded under the case-folded channel name and
// consumed (removed) by take, so one JOIN echo persists it exactly once.
func TestPendingJoinKeyRecordAndTake(t *testing.T) {
	c := newRejoinTestClient(t)

	c.recordPendingJoinKey("#Secret", "hunter2")

	key, ok := c.takePendingJoinKey("#secret")
	if !ok || key != "hunter2" {
		t.Fatalf("takePendingJoinKey = %q, %v; want hunter2, true", key, ok)
	}
	if _, ok := c.takePendingJoinKey("#secret"); ok {
		t.Fatal("second take should find nothing (entry consumed)")
	}
}

func TestPendingJoinKeyAbsent(t *testing.T) {
	c := newRejoinTestClient(t)
	if _, ok := c.takePendingJoinKey("#nokey"); ok {
		t.Fatal("takePendingJoinKey on empty map should report absent")
	}
}

// On our own JOIN echo, the key we joined with is written to the channel row so
// auto-rejoin can use it after a reconnect. An empty pending key clears a stale
// stored key (the join succeeded without one, so none is needed).
func TestPersistPendingJoinKey(t *testing.T) {
	c := newRejoinTestClient(t)

	ch := &storage.Channel{NetworkID: c.networkID, Name: "#secret", CreatedAt: time.Now()}
	mustCreateChannel(t, c.storage, ch)

	c.recordPendingJoinKey("#secret", "hunter2")
	c.persistPendingJoinKey("#Secret", ch)

	got, err := c.storage.GetChannelByName(c.networkID, "#secret")
	if err != nil {
		t.Fatalf("GetChannelByName: %v", err)
	}
	if got.Key != "hunter2" {
		t.Errorf("stored Key = %q, want hunter2", got.Key)
	}
	if ch.Key != "hunter2" {
		t.Errorf("in-memory ch.Key = %q, want hunter2", ch.Key)
	}

	// Rejoining without a key (successfully) clears the stored key.
	c.recordPendingJoinKey("#secret", "")
	c.persistPendingJoinKey("#secret", ch)
	got, _ = c.storage.GetChannelByName(c.networkID, "#secret")
	if got.Key != "" {
		t.Errorf("stored Key = %q after keyless join, want empty", got.Key)
	}
}

// A JOIN we did not initiate through JoinChannelWithKey (e.g. auto-rejoin)
// leaves the stored key untouched.
func TestPersistPendingJoinKeyNoPendingIsNoop(t *testing.T) {
	c := newRejoinTestClient(t)

	ch := &storage.Channel{NetworkID: c.networkID, Name: "#secret", Key: "hunter2", CreatedAt: time.Now()}
	mustCreateChannel(t, c.storage, ch)

	c.persistPendingJoinKey("#secret", ch)

	got, _ := c.storage.GetChannelByName(c.networkID, "#secret")
	if got.Key != "hunter2" {
		t.Errorf("stored Key = %q, want hunter2 untouched", got.Key)
	}
}

// channelsToJoin carries the stored key through both selection paths so
// doAutoJoin can send "JOIN <chan> <key>".
func TestChannelsToJoinCarriesKey(t *testing.T) {
	c := newRejoinTestClient(t)

	mustCreateChannel(t, c.storage, &storage.Channel{
		NetworkID: c.networkID, Name: "#keyed", Key: "hunter2",
		AutoJoin: true, IsOpen: true, CreatedAt: time.Now(),
	})

	for _, reconnect := range []bool{false, true} {
		channels, err := c.channelsToJoin(reconnect)
		if err != nil {
			t.Fatalf("channelsToJoin(%v): %v", reconnect, err)
		}
		if len(channels) != 1 || channels[0].Key != "hunter2" {
			t.Errorf("channelsToJoin(%v) = %+v, want #keyed with key hunter2", reconnect, channels)
		}
	}
}
