package storage

import (
	"path/filepath"
	"testing"
	"time"
)

type accountEvidenceStore interface {
	ReconcilePMIdentity(int64, string, string, string, string, bool) (*PrivateMessageConversation, bool, error)
}

func TestAccountBindingSurvivesClosedClientAndMissedRename(t *testing.T) {
	path := filepath.Join(t.TempDir(), "contacts.db")
	s, err := NewStorage(path, 100, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	n := makeNetwork("any-network")
	if err := s.CreateNetwork(n); err != nil {
		t.Fatal(err)
	}
	original, _, err := s.GetOrCreatePMConversation(n.ID, "alice", "me")
	if err != nil {
		t.Fatal(err)
	}
	evidence, ok := any(s).(accountEvidenceStore)
	if !ok {
		t.Fatal("storage cannot persist server-verified account identity")
	}
	if _, _, err := evidence.ReconcilePMIdentity(n.ID, "alice", "alice", "alice-account", "account-tag", false); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = NewStorage(path, 100, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	recovered, _, err := any(s).(accountEvidenceStore).ReconcilePMIdentity(n.ID, "spookyAlice", "spookyalice", "alice-account", "whox", false)
	if err != nil || recovered == nil || recovered.ID != original.ID {
		t.Fatalf("fresh account evidence did not recover saved contact: original=%d, recovered=%+v, error=%v", original.ID, recovered, err)
	}
}

func TestPMRenamePreservesConversationIdentity(t *testing.T) {
	s := newTestStorage(t)
	n := makeNetwork("seasons")
	if err := s.CreateNetwork(n); err != nil {
		t.Fatal(err)
	}
	before, _, err := s.GetOrCreatePMConversation(n.ID, "alice", "me")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RenamePMConversation(n.ID, "alice", "spookyAlice"); err != nil {
		t.Fatal(err)
	}
	conversations, err := s.GetOpenPMConversations(n.ID, "me")
	if err != nil {
		t.Fatal(err)
	}
	if len(conversations) != 1 || conversations[0].ID != before.ID {
		t.Fatalf("rename replaced the saved conversation: before ID=%d, after=%+v", before.ID, conversations)
	}
}

func TestPMHistoryDeduplicatesAcrossRenamedAliases(t *testing.T) {
	s := newTestStorage(t)
	n := makeNetwork("seasons")
	if err := s.CreateNetwork(n); err != nil {
		t.Fatal(err)
	}
	contact, _, err := s.GetOrCreatePMConversation(n.ID, "alice", "me")
	if err != nil {
		t.Fatal(err)
	}
	message := Message{NetworkID: n.ID, ConversationID: contact.ID, PMTarget: "alice", User: "alice", Message: "same server message", MessageType: "privmsg", Timestamp: time.Now(), MsgID: "stable-msgid"}
	if _, err := s.WriteHistoryMessages([]Message{message}); err != nil {
		t.Fatal(err)
	}
	message.PMTarget = "spookyAlice"
	if _, err := s.WriteHistoryMessages([]Message{message}); err != nil {
		t.Fatal(err)
	}
	history, err := s.GetPrivateMessages(n.ID, PMReference(contact.ID), "me", 20)
	if err != nil || len(history) != 1 {
		t.Fatalf("replayed renamed aliases duplicated a message: %+v, %v", history, err)
	}
}

func TestContactAliasLookupRetainsIRCCaseCompatibility(t *testing.T) {
	s := newTestStorage(t)
	n := makeNetwork("seasons")
	if err := s.CreateNetwork(n); err != nil {
		t.Fatal(err)
	}
	contact, _, err := s.GetOrCreatePMConversation(n.ID, "alice", "me")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetPMContactTarget(n.ID, contact.ID, "spookyAlice", "spookyalice"); err != nil {
		t.Fatal(err)
	}
	got, err := s.FindPMConversation(n.ID, "SPOOKYALICE")
	if err != nil || got.ID != contact.ID {
		t.Fatalf("case-compatible alias lookup = %+v, %v", got, err)
	}
}

func TestSearchResultsRetainPrivateConversationOwnership(t *testing.T) {
	s := newTestStorage(t)
	n := makeNetwork("seasons")
	if err := s.CreateNetwork(n); err != nil {
		t.Fatal(err)
	}
	contact, _, err := s.GetOrCreatePMConversation(n.ID, "alice", "me")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.WriteMessageSync(Message{NetworkID: n.ID, PMTarget: "alice", ConversationID: contact.ID, User: "alice", Message: "pumpkin", MessageType: "privmsg", Timestamp: time.Now()}); err != nil {
		t.Fatal(err)
	}
	results, err := s.SearchMessages("pumpkin", nil, 20)
	if err != nil || len(results) != 1 || results[0].ConversationID != contact.ID {
		t.Fatalf("search lost PM identity: %+v, %v", results, err)
	}
}

func TestLegacyContactMigrationPreservesIDsHistoryAndNicknameWatches(t *testing.T) {
	s := newTestStorage(t)
	n := makeNetwork("migration")
	if err := s.CreateNetwork(n); err != nil {
		t.Fatal(err)
	}
	contact, _, err := s.GetOrCreatePMConversation(n.ID, "alice", "me")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.WriteMessageSync(Message{NetworkID: n.ID, PMTarget: "alice", User: "alice", Message: "saved history", MessageType: "privmsg", Timestamp: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := s.AddMonitoredNick(n.ID, "alice"); err != nil {
		t.Fatal(err)
	}
	// Recreate the previous public database schema while retaining its saved data.
	for _, sql := range []string{
		`DROP INDEX IF EXISTS idx_messages_contact_msgid`,
		`DROP INDEX IF EXISTS idx_messages_conversation`,
		`ALTER TABLE messages DROP COLUMN conversation_id`,
		`CREATE TABLE legacy_pm(id INTEGER PRIMARY KEY AUTOINCREMENT,network_id INTEGER NOT NULL,target_user TEXT NOT NULL,is_open BOOLEAN NOT NULL DEFAULT 0,created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,updated_at TIMESTAMP,UNIQUE(network_id,target_user))`,
		`INSERT INTO legacy_pm SELECT id,network_id,target_user,is_open,created_at,updated_at FROM private_message_conversations`,
		`DROP TABLE private_message_conversations`,
		`ALTER TABLE legacy_pm RENAME TO private_message_conversations`,
	} {
		if _, err := s.db.Exec(sql); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		if err := Migrate(s.db); err != nil {
			t.Fatalf("migration pass %d: %v", i, err)
		}
	}
	contacts, err := s.GetOpenPMConversations(n.ID, "me")
	if err != nil || len(contacts) != 1 || contacts[0].ID != contact.ID || contacts[0].Account != "" {
		t.Fatalf("migration changed identity: %+v, %v", contacts, err)
	}
	history, err := s.GetPrivateMessages(n.ID, PMReference(contact.ID), "me", 20)
	if err != nil || len(history) != 1 || history[0].Message != "saved history" || history[0].ConversationID != contact.ID {
		t.Fatalf("migration lost history: %+v, %v", history, err)
	}
	watch, err := s.GetMonitoredNicks(n.ID)
	if err != nil || len(watch) != 1 || watch[0] != "alice" {
		t.Fatalf("migration changed explicit nickname watch: %v, %v", watch, err)
	}
}

func TestAccountBindingsRemainScopedToTheirNetwork(t *testing.T) {
	s := newTestStorage(t)
	var ids []int64
	for _, name := range []string{"one", "two"} {
		n := makeNetwork(name)
		if err := s.CreateNetwork(n); err != nil {
			t.Fatal(err)
		}
		contact, _, err := s.ReconcilePMIdentity(n.ID, "alice", "alice", "same-account", "account-tag", true)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, contact.ID)
	}
	if ids[0] == ids[1] {
		t.Fatal("same account text merged two networks")
	}
}
