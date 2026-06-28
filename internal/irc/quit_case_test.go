package irc

import (
	"slices"
	"testing"
	"time"

	"github.com/matt0x6f/irc-client/internal/storage"
)

// A QUIT whose source nick differs only in case from the stored roster entry must
// still remove that user. IRC nick equality is case-insensitive (per the server's
// CASEMAPPING), so a case-sensitive comparison would leave a ghost lingering in
// the user list after they quit.
func TestQuitRemovesUserCaseInsensitively(t *testing.T) {
	c := newNickTestClient(t) // preferred nick matt0x6f

	ch := &storage.Channel{NetworkID: c.networkID, Name: "#a", IsOpen: true, CreatedAt: time.Now()}
	if err := c.storage.CreateChannel(ch); err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}
	for _, n := range []string{"Bob", "alice"} {
		if err := c.storage.AddChannelUser(ch.ID, n, ""); err != nil {
			t.Fatalf("AddChannelUser(%q): %v", n, err)
		}
	}

	// Bob quits, but the QUIT source nick arrives in a different case.
	c.handleQuit(parseLine(t, ":BOB!u@h QUIT :bye"))

	users, err := c.storage.GetChannelUsers(ch.ID)
	if err != nil {
		t.Fatalf("GetChannelUsers: %v", err)
	}
	nicks := make([]string, len(users))
	for i, u := range users {
		nicks[i] = u.Nickname
	}
	if slices.Contains(nicks, "Bob") {
		t.Fatalf("Bob should have been removed on QUIT, got %v", nicks)
	}
	if !slices.Contains(nicks, "alice") {
		t.Fatalf("alice should be untouched, got %v", nicks)
	}
}
