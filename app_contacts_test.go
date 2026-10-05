package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/matt0x6f/irc-client/internal/events"
	"github.com/matt0x6f/irc-client/internal/storage"
)

func TestDisconnectedContactSnapshotRetainsIdentityAndUnknownPresence(t *testing.T) {
	s, err := storage.NewStorage(filepath.Join(t.TempDir(), "contacts.db"), 100, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	n := makeAppTestNetwork(t, s, "any-network")
	original, _, err := s.GetOrCreatePMConversation(n.ID, "alice", n.Nickname)
	if err != nil {
		t.Fatal(err)
	}
	a := &App{storage: s}
	api, ok := any(a).(interface {
		GetPrivateContacts(int64) ([]storage.PrivateMessageConversation, error)
	})
	if !ok {
		t.Fatal("the sidebar API still exposes nickname-only conversations")
	}
	contacts, err := api.GetPrivateContacts(n.ID)
	if err != nil || len(contacts) != 1 || contacts[0].ID != original.ID || contacts[0].Reference != storage.PMReference(original.ID) || contacts[0].Presence != "unknown" {
		t.Fatalf("disconnected contact snapshot = %+v, %v", contacts, err)
	}
}

func TestOpenPrivateContactReturnsStablePaneReference(t *testing.T) {
	s, err := storage.NewStorage(filepath.Join(t.TempDir(), "contacts.db"), 100, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	n := makeAppTestNetwork(t, s, "any-network")
	a := &App{storage: s, eventBus: events.NewEventBus()}
	api, ok := any(a).(interface {
		OpenPrivateContact(int64, string) (*storage.PrivateMessageConversation, error)
	})
	if !ok {
		t.Fatal("opening a DM still returns no stable identity")
	}
	contact, err := api.OpenPrivateContact(n.ID, "alice")
	if err != nil || contact == nil || contact.ID == 0 || contact.Reference != storage.PMReference(contact.ID) {
		t.Fatalf("opened contact = %+v, %v", contact, err)
	}
	if contact.Presence != "unknown" || contact.Sessions == nil {
		t.Fatalf("opening a disconnected contact returned an incomplete presence snapshot: %+v", contact)
	}
	reopened, err := api.OpenPrivateContact(n.ID, contact.Reference)
	if err != nil || reopened.ID != contact.ID {
		t.Fatalf("reopening changed identity: %+v, %v", reopened, err)
	}
}

func TestRestoredPrivatePaneUsesConversationIdentity(t *testing.T) {
	s, err := storage.NewStorage(filepath.Join(t.TempDir(), "contacts.db"), 100, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	n := makeAppTestNetwork(t, s, "any-network")
	contact, _, err := s.GetOrCreatePMConversation(n.ID, "alice", n.Nickname)
	if err != nil {
		t.Fatal(err)
	}
	a := &App{storage: s}
	pane, err := a.GetLastOpenPane()
	if err != nil || pane == nil || pane.Name != storage.PMReference(contact.ID) {
		t.Fatalf("restored pane uses nickname instead of identity: %+v, %v", pane, err)
	}
}
