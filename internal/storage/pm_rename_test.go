package storage

import (
	"fmt"
	"testing"
	"time"
)

func TestRenamePMConversationKeepsHistoryAndNetworkScope(t *testing.T) {
	s := newTestStorage(t)
	network, other := makeNetwork("autumn"), makeNetwork("other")
	for _, n := range []*Network{network, other} {
		if err := s.CreateNetwork(n); err != nil {
			t.Fatal(err)
		}
		if _, _, err := s.GetOrCreatePMConversation(n.ID, "Alice", "me"); err != nil {
			t.Fatal(err)
		}
		// Deliberately buffered: the rename must include the last pre-NICK PM.
		if err := s.WriteMessage(Message{NetworkID: n.ID, User: "Alice", PMTarget: "alice", Message: "boo", MessageType: "privmsg", Timestamp: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	changed, err := s.RenamePMConversation(network.ID, "ALICE", "spookyAlice")
	if err != nil || !changed {
		t.Fatalf("RenamePMConversation = %v, %v", changed, err)
	}
	msgs, err := s.GetPrivateMessages(network.ID, "spookyalice", "me", 10)
	if err != nil || len(msgs) != 1 || msgs[0].User != "Alice" || msgs[0].Message != "boo" {
		t.Fatalf("renamed history = %+v, %v", msgs, err)
	}
	old, err := s.GetPrivateMessages(network.ID, "alice", "me", 10)
	if err != nil || len(old) != 0 {
		t.Fatalf("old target history = %+v, %v", old, err)
	}
	peers, err := s.GetPrivateMessageConversations(other.ID, "me", true)
	if err != nil || len(peers) != 1 || peers[0] != "Alice" {
		t.Fatalf("other network peers = %v, %v", peers, err)
	}
}

func TestRenamePMConversationPreservesDifferentSavedHistory(t *testing.T) {
	s := newTestStorage(t)
	n := makeNetwork("autumn")
	if err := s.CreateNetwork(n); err != nil {
		t.Fatal(err)
	}
	alice, _, err := s.GetOrCreatePMConversation(n.ID, "alice", "me")
	if err != nil {
		t.Fatal(err)
	}
	previous, _, err := s.GetOrCreatePMConversation(n.ID, "spookyalice", "me")
	if err != nil {
		t.Fatal(err)
	}
	for _, msg := range []Message{
		{NetworkID: n.ID, User: "alice", PMTarget: "alice", Message: "Alice's history", MessageType: "privmsg", Timestamp: time.Now()},
		{NetworkID: n.ID, User: "spookyalice", PMTarget: "spookyalice", Message: "Someone else's history", MessageType: "privmsg", Timestamp: time.Now()},
	} {
		if err := s.WriteMessageSync(msg); err != nil {
			t.Fatal(err)
		}
	}
	if changed, err := s.RenamePMConversation(n.ID, "alice", "spookyalice"); err != nil || !changed {
		t.Fatalf("rename = %v, %v", changed, err)
	}
	convs, err := s.GetOpenPMConversations(n.ID, "me")
	if err != nil || len(convs) != 2 {
		t.Fatalf("rename merged unrelated conversations: %+v, %v", convs, err)
	}
	for _, want := range []struct {
		id   int64
		text string
	}{{alice.ID, "Alice's history"}, {previous.ID, "Someone else's history"}} {
		msgs, err := s.GetPrivateMessages(n.ID, fmt.Sprintf("@%d", want.id), "me", 10)
		if err != nil || len(msgs) != 1 || msgs[0].Message != want.text {
			t.Fatalf("conversation %d history = %+v, %v", want.id, msgs, err)
		}
	}
}

func TestRenamePMConversationDoesNotCreateUnrelatedChats(t *testing.T) {
	s := newTestStorage(t)
	n := makeNetwork("autumn")
	if err := s.CreateNetwork(n); err != nil {
		t.Fatal(err)
	}
	if changed, err := s.RenamePMConversation(n.ID, "unknown", "spooky"); err != nil || changed {
		t.Fatalf("unrelated rename = %v, %v", changed, err)
	}
}
