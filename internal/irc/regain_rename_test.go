package irc

import (
	"slices"
	"testing"
	"time"

	"github.com/matt0x6f/irc-client/internal/storage"
)

func regainChannelNicks(t *testing.T, c *IRCClient, name string) []string {
	t.Helper()
	ch, err := c.storage.GetChannelByName(c.networkID, name)
	if err != nil {
		t.Fatalf("GetChannelByName(%q): %v", name, err)
	}
	us, err := c.storage.GetChannelUsers(ch.ID)
	if err != nil {
		t.Fatalf("GetChannelUsers(%q): %v", name, err)
	}
	out := make([]string, len(us))
	for i, u := range us {
		out[i] = u.Nickname
	}
	return out
}

func regainSeedChannel(t *testing.T, c *IRCClient, name string, nicks ...string) {
	t.Helper()
	ch := &storage.Channel{NetworkID: c.networkID, Name: name, IsOpen: true, CreatedAt: time.Now()}
	if err := c.storage.CreateChannel(ch); err != nil {
		t.Fatalf("CreateChannel(%q): %v", name, err)
	}
	for _, n := range nicks {
		if err := c.storage.AddChannelUser(ch.ID, n, ""); err != nil {
			t.Fatalf("AddChannelUser(%q,%q): %v", name, n, err)
		}
	}
}

// Reclaiming our preferred nick while the freed ghost still occupies that nick in
// a shared channel must rename us in EVERY channel, not abort. The rename is one
// network-wide UPDATE under UNIQUE(channel_id, nickname); a naive UPDATE aborts
// the whole statement on the shared-channel collision and leaves us listed under
// the old alt nick everywhere. The ghost entry is replaced by us.
func TestRegainRenameReplacesGhostAcrossChannels(t *testing.T) {
	c := newNickTestClient(t) // preferred nick matt0x6f
	c.currentNick = "matt0x6f_2"

	// #a still has the ghost (matt0x6f) alongside us; #b has only us.
	regainSeedChannel(t, c, "#a", "matt0x6f", "matt0x6f_2", "alice")
	regainSeedChannel(t, c, "#b", "matt0x6f_2", "bob")

	c.handleNickMessage(parseLine(t, ":matt0x6f_2!u@h NICK matt0x6f"))

	a := regainChannelNicks(t, c, "#a")
	b := regainChannelNicks(t, c, "#b")

	// #a: ghost replaced, we are now matt0x6f, alice untouched, no stale alt.
	if !slices.Contains(a, "matt0x6f") || !slices.Contains(a, "alice") || slices.Contains(a, "matt0x6f_2") {
		t.Fatalf("#a = %v, want [alice matt0x6f] (ghost replaced, no stale matt0x6f_2)", a)
	}
	if len(a) != 2 {
		t.Fatalf("#a = %v, want exactly 2 members (no duplicate matt0x6f)", a)
	}
	// #b: renamed cleanly even though the collision was in a different channel.
	if !slices.Contains(b, "matt0x6f") || slices.Contains(b, "matt0x6f_2") {
		t.Fatalf("#b = %v, want [bob matt0x6f] (rename must not be aborted by #a's collision)", b)
	}
}
