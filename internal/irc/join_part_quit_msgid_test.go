package irc

import (
	"testing"
	"time"

	"github.com/matt0x6f/irc-client/internal/storage"
)

// latestMessageOfType returns the most recently stored message of the given
// messageType for the channel, failing the test if none is found.
func latestMessageOfType(t *testing.T, s *storage.Storage, networkID, channelID int64, messageType string) storage.Message {
	t.Helper()
	msgs, err := s.GetMessages(networkID, &channelID, 50)
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].MessageType == messageType {
			return msgs[i]
		}
	}
	t.Fatalf("no stored message of type %q found (got %d messages)", messageType, len(msgs))
	return storage.Message{}
}

// TestLiveJoinPersistsMsgID is the regression guard for replay dedup: a live
// JOIN carrying the IRCv3 @msgid tag must persist that msgid on the stored
// "join" row. Without it, a later replayed copy of the same JOIN can never
// dedup against the live copy and the event double-prints.
func TestLiveJoinPersistsMsgID(t *testing.T) {
	c := newNickTestClient(t) // preferred nick matt0x6f

	e := parseLine(t, "@msgid=join-xyz :alice!u@h JOIN #room")
	c.handleJoin(e)

	ch, err := c.storage.GetChannelByName(c.networkID, "#room")
	if err != nil {
		t.Fatalf("GetChannelByName(#room): %v", err)
	}

	row := latestMessageOfType(t, c.storage, c.networkID, ch.ID, "join")
	if row.MsgID != "join-xyz" {
		t.Fatalf("MsgID = %q, want %q", row.MsgID, "join-xyz")
	}
}

// TestLivePartPersistsMsgID mirrors TestLiveJoinPersistsMsgID for PART.
func TestLivePartPersistsMsgID(t *testing.T) {
	c := newNickTestClient(t)

	ch := &storage.Channel{NetworkID: c.networkID, Name: "#room", IsOpen: true, CreatedAt: time.Now()}
	if err := c.storage.CreateChannel(ch); err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}
	if err := c.storage.AddChannelUser(ch.ID, "alice", ""); err != nil {
		t.Fatalf("AddChannelUser: %v", err)
	}

	e := parseLine(t, "@msgid=part-xyz :alice!u@h PART #room :bye")
	c.handlePart(e)

	row := latestMessageOfType(t, c.storage, c.networkID, ch.ID, "part")
	if row.MsgID != "part-xyz" {
		t.Fatalf("MsgID = %q, want %q", row.MsgID, "part-xyz")
	}
}

// TestLiveQuitPersistsMsgID mirrors TestLiveJoinPersistsMsgID for QUIT. The
// same msgid is written to every channel row the quitting user was in — safe
// because the dedup index is per-conversation, so identical msgids on
// different channel rows don't collide.
func TestLiveQuitPersistsMsgID(t *testing.T) {
	c := newNickTestClient(t)

	ch := &storage.Channel{NetworkID: c.networkID, Name: "#room", IsOpen: true, CreatedAt: time.Now()}
	if err := c.storage.CreateChannel(ch); err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}
	if err := c.storage.AddChannelUser(ch.ID, "alice", ""); err != nil {
		t.Fatalf("AddChannelUser: %v", err)
	}

	e := parseLine(t, "@msgid=quit-xyz :alice!u@h QUIT :bye")
	c.handleQuit(e)

	row := latestMessageOfType(t, c.storage, c.networkID, ch.ID, "quit")
	if row.MsgID != "quit-xyz" {
		t.Fatalf("MsgID = %q, want %q", row.MsgID, "quit-xyz")
	}
}
