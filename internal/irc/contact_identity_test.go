package irc

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/ergochat/irc-go/ircevent"
	"github.com/matt0x6f/irc-client/internal/events"
	"github.com/matt0x6f/irc-client/internal/storage"
)

func TestHistoricalAccountTagsDoNotBecomeCurrentSessionEvidence(t *testing.T) {
	c, _ := newWhoxBotTestClient(t)
	c.enabledCaps["account-tag"] = true
	contact, _, _ := c.storage.GetOrCreatePMConversation(c.networkID, "alice", c.network.Nickname)
	c.handlePrivmsg(parseLine(t, "@account=alice-account :alice!u@h PRIVMSG matt0x6f :original"))
	c.handlePrivmsg(parseLine(t, "@account=bob-account :alice!other@h PRIVMSG matt0x6f :replacement"))
	batch := &ircevent.Batch{Message: parseLine(t, "BATCH +1 chathistory alice"), Items: []*ircevent.Batch{
		mustParseBatchItem(t, "@account=alice-account;msgid=historic;time=2024-06-14T10:00:00.000Z :alice!u@h PRIVMSG matt0x6f :older Alice"),
	}}
	c.handleChatHistoryBatch(batch)
	history, err := c.storage.GetPrivateMessages(c.networkID, storage.PMReference(contact.ID), c.network.Nickname, 20)
	if err != nil || len(history) != 2 {
		t.Fatalf("historical account message lost its contact ownership: %+v, %v", history, err)
	}
	if target, err := c.ResolvePrivateTarget(storage.PMReference(contact.ID)); err == nil {
		t.Fatalf("history established a current recipient %q", target)
	}
}

func TestHistoricalAccountTagsRequireNegotiatedSupport(t *testing.T) {
	c, _ := newWhoxBotTestClient(t)
	c.enabledCaps["account-tag"] = true
	contact, _, _ := c.storage.GetOrCreatePMConversation(c.networkID, "alice", c.network.Nickname)
	c.handlePrivmsg(parseLine(t, "@account=alice-account :alice!u@h PRIVMSG matt0x6f :original"))
	c.enabledCaps["account-tag"] = false
	_, accepted := c.buildHistoryChatMessage(parseLine(t, "@account=alice-account :alice!u@h PRIVMSG matt0x6f :unnegotiated replay"))
	if accepted {
		t.Fatal("unnegotiated historical tag inherited an account-bound conversation")
	}
	history, err := c.storage.GetPrivateMessages(c.networkID, storage.PMReference(contact.ID), c.network.Nickname, 20)
	if err != nil || len(history) != 1 {
		t.Fatalf("local history changed: %+v, %v", history, err)
	}
}

func TestUnrelatedMetadataCannotRestoreLostAccountEvidence(t *testing.T) {
	c, _ := newWhoxBotTestClient(t)
	c.enabledCaps["account-tag"] = true
	contact, _, _ := c.storage.GetOrCreatePMConversation(c.networkID, "alice", c.network.Nickname)
	c.handlePrivmsg(parseLine(t, "@account=alice-account :alice!u@h PRIVMSG matt0x6f :identified"))
	c.enabledCaps["account-tag"] = false
	c.handlePrivmsg(parseLine(t, ":alice!other@h PRIVMSG matt0x6f :no account evidence"))
	c.enabledCaps["account-tag"] = true
	c.handleAway(parseLine(t, ":alice!other@h AWAY :away"))
	if target, err := c.ResolvePrivateTarget(storage.PMReference(contact.ID)); err == nil {
		t.Fatalf("AWAY reused stale account metadata to route to %q", target)
	}
}

func TestServerAccountEvidenceRecoversContactWithoutNickEvent(t *testing.T) {
	c, _ := newWhoxBotTestClient(t)
	c.enabledCaps["account-notify"] = true
	original, _, err := c.storage.GetOrCreatePMConversation(c.networkID, "alice", c.network.Nickname)
	if err != nil {
		t.Fatal(err)
	}
	c.handleAccount(parseLine(t, ":alice!u@h ACCOUNT alice-account"))
	contacts, err := c.storage.GetOpenPMConversations(c.networkID, c.network.Nickname)
	if err != nil || len(contacts) != 1 || contacts[0].Account != "alice-account" {
		t.Fatalf("ACCOUNT did not persist verified contact binding: %+v, %v", contacts, err)
	}
	// A new client has no live rename history. Its first WHOX roster identifies
	// the existing account under a new nickname on an arbitrary network.
	reconnected := NewIRCClient(c.network, events.NewEventBus(), c.storage)
	reconnected.networkID = c.networkID
	reconnected.supportsWHOX = true
	reconnected.handleWhoxReply(parseLine(t, ":srv 354 me 332 #chan u h spookyAlice H alice-account :Alice"))
	contacts, err = reconnected.storage.GetOpenPMConversations(c.networkID, c.network.Nickname)
	if err != nil || len(contacts) != 1 || contacts[0].ID != original.ID || contacts[0].TargetUser != "spookyAlice" {
		t.Fatalf("fresh WHOX did not recover original DM: %+v, %v", contacts, err)
	}
}

func TestLearningLegacyAccountUsesNegotiatedNickCaseMapping(t *testing.T) {
	c, _ := newWhoxBotTestClient(t)
	c.enabledCaps["account-tag"] = true
	contact, _, err := c.storage.GetOrCreatePMConversation(c.networkID, "[Alice]", c.network.Nickname)
	if err != nil {
		t.Fatal(err)
	}
	c.handlePrivmsg(parseLine(t, "@account=alice-account :{ALICE}!u@h PRIVMSG matt0x6f :hello"))
	contacts, err := c.storage.GetOpenPMConversations(c.networkID, c.network.Nickname)
	if err != nil || len(contacts) != 1 || contacts[0].ID != contact.ID || contacts[0].Account != "alice-account" {
		t.Fatalf("IRC-equivalent legacy nickname split its saved identity: %+v, %v", contacts, err)
	}
}

func TestPrivateContactSnapshotUsesStableReferenceAndVerifiedPresence(t *testing.T) {
	c, _ := newWhoxBotTestClient(t)
	c.enabledCaps["account-notify"] = true
	c.connected = true
	original, _, err := c.storage.GetOrCreatePMConversation(c.networkID, "alice", c.network.Nickname)
	if err != nil {
		t.Fatal(err)
	}
	c.handleAccount(parseLine(t, ":alice!u@h ACCOUNT alice-account"))
	snapshots, ok := any(c).(interface {
		PrivateContacts() ([]storage.PrivateMessageConversation, error)
	})
	if !ok {
		t.Fatal("sidebar has no contact identity snapshot")
	}
	contacts, err := snapshots.PrivateContacts()
	if err != nil || len(contacts) != 1 {
		t.Fatalf("snapshot = %+v, %v", contacts, err)
	}
	encoded, err := json.Marshal(contacts[0])
	if err != nil {
		t.Fatal(err)
	}
	var row map[string]any
	if err := json.Unmarshal(encoded, &row); err != nil {
		t.Fatal(err)
	}
	if row["reference"] != fmt.Sprintf("@%d", original.ID) || row["presence"] != "online" || row["target"] != "alice" {
		t.Fatalf("contact snapshot lost stable identity/current presence: %s", encoded)
	}
}

func TestObservedNickChangeUpdatesVerifiedContactRecipient(t *testing.T) {
	c, _ := newWhoxBotTestClient(t)
	c.enabledCaps["account-notify"] = true
	original, _, err := c.storage.GetOrCreatePMConversation(c.networkID, "alice", c.network.Nickname)
	if err != nil {
		t.Fatal(err)
	}
	c.handleAccount(parseLine(t, ":alice!u@h ACCOUNT alice-account"))
	c.handleNickMessage(parseLine(t, ":alice!u@h NICK spookyAlice"))
	target, err := c.ResolvePrivateTarget(storage.PMReference(original.ID))
	if err != nil || target != "spookyAlice" {
		t.Fatalf("live rename recipient = %q, %v", target, err)
	}
}

func TestSendToSavedContactRejectsUnresolvedAccountBeforeWire(t *testing.T) {
	c, _ := newWhoxBotTestClient(t)
	c.enabledCaps["account-notify"] = true
	original, _, err := c.storage.GetOrCreatePMConversation(c.networkID, "alice", c.network.Nickname)
	if err != nil {
		t.Fatal(err)
	}
	c.handleAccount(parseLine(t, ":alice!u@h ACCOUNT alice-account"))
	c.handleAccount(parseLine(t, ":alice!other@h ACCOUNT bob-account"))
	c.connected = true
	defer func() {
		if value := recover(); value != nil {
			t.Fatalf("unresolved contact reached the wire sender: %v", value)
		}
	}()
	err = c.SendMessage(storage.PMReference(original.ID), "private reply")
	if err == nil || !strings.Contains(err.Error(), "current nickname is unknown") {
		t.Fatalf("send did not reject unresolved identity: %v", err)
	}
}

func TestContactPresenceRequiresFreshIdentityAfterReconnect(t *testing.T) {
	c, _ := newWhoxBotTestClient(t)
	c.enabledCaps["account-notify"] = true
	c.connected = true
	original, _, err := c.storage.GetOrCreatePMConversation(c.networkID, "alice", c.network.Nickname)
	if err != nil {
		t.Fatal(err)
	}
	c.handleAccount(parseLine(t, ":alice!u@h ACCOUNT alice-account"))
	presence, ok := any(c).(interface{ ContactPresence(string) (string, error) })
	if !ok {
		t.Fatal("contact presence is still keyed only by nickname")
	}
	if state, err := presence.ContactPresence(storage.PMReference(original.ID)); err != nil || state != "online" {
		t.Fatalf("verified contact presence = %q, %v", state, err)
	}
	reconnected := NewIRCClient(c.network, events.NewEventBus(), c.storage)
	reconnected.networkID = c.networkID
	reconnected.connected = true
	reconnected.supportsWHOX = true
	fresh := any(reconnected).(interface{ ContactPresence(string) (string, error) })
	if state, err := fresh.ContactPresence(storage.PMReference(original.ID)); err != nil || state != "unknown" {
		t.Fatalf("unresolved reconnect presence = %q, %v", state, err)
	}
	reconnected.handleWhoxReply(parseLine(t, ":srv 354 me 332 #chan u h spookyAlice H alice-account :Alice"))
	if state, err := fresh.ContactPresence(storage.PMReference(original.ID)); err != nil || state != "online" {
		t.Fatalf("rediscovered account presence = %q, %v", state, err)
	}
	reconnected.connected = false
	if state, err := fresh.ContactPresence(storage.PMReference(original.ID)); err != nil || state != "unknown" {
		t.Fatalf("disconnected contact presence = %q, %v", state, err)
	}
}

func TestReusedNickDoesNotAppendAnotherAccountsMessages(t *testing.T) {
	c, _ := newWhoxBotTestClient(t)
	c.enabledCaps["account-tag"] = true
	original, _, err := c.storage.GetOrCreatePMConversation(c.networkID, "alice", c.network.Nickname)
	if err != nil {
		t.Fatal(err)
	}
	c.handlePrivmsg(parseLine(t, "@account=alice-account :alice!u@h PRIVMSG matt0x6f :Alice's message"))
	c.handlePrivmsg(parseLine(t, "@account=bob-account :alice!other@elsewhere PRIVMSG matt0x6f :Bob's message"))
	history, err := c.storage.GetPrivateMessages(c.networkID, storage.PMReference(original.ID), c.network.Nickname, 20)
	if err != nil || len(history) != 1 || history[0].Message != "Alice's message" {
		t.Fatalf("nickname reuse mixed accounts' histories: %+v, %v", history, err)
	}
	contacts, err := c.storage.GetOpenPMConversations(c.networkID, c.network.Nickname)
	if err != nil || len(contacts) != 2 {
		t.Fatalf("replacement account did not receive a separate contact: %+v, %v", contacts, err)
	}
}

func TestMultipleAccountSessionsRetainPreferredRecipient(t *testing.T) {
	c, _ := newWhoxBotTestClient(t)
	c.supportsWHOX = true
	original, _, err := c.storage.GetOrCreatePMConversation(c.networkID, "alice", c.network.Nickname)
	if err != nil {
		t.Fatal(err)
	}
	c.handleWhoxReply(parseLine(t, ":srv 354 me 332 #chan u h alice H alice-account :Alice"))
	c.handleWhoxReply(parseLine(t, ":srv 354 me 332 #chan u h ghostAlice H alice-account :Alice"))
	contacts, err := c.storage.GetOpenPMConversations(c.networkID, c.network.Nickname)
	if err != nil || len(contacts) != 1 || contacts[0].ID != original.ID || contacts[0].TargetUser != "alice" {
		t.Fatalf("additional session displaced preferred recipient: %+v, %v", contacts, err)
	}
}

func TestRenamingAnotherSessionDoesNotDisplacePreferredRecipient(t *testing.T) {
	c, _ := newWhoxBotTestClient(t)
	c.supportsWHOX = true
	contact, _, err := c.storage.GetOrCreatePMConversation(c.networkID, "alice", c.network.Nickname)
	if err != nil {
		t.Fatal(err)
	}
	c.handleWhoxReply(parseLine(t, ":srv 354 me 332 #chan u h alice H alice-account :Alice"))
	c.handleWhoxReply(parseLine(t, ":srv 354 me 332 #chan u h ghostAlice H alice-account :Alice"))
	c.handleNickMessage(parseLine(t, ":ghostAlice!u@h NICK scarierAlice"))
	target, err := c.ResolvePrivateTarget(storage.PMReference(contact.ID))
	if err != nil || target != "alice" {
		t.Fatalf("another session's rename displaced preferred recipient: %q, %v", target, err)
	}
}

func TestSavedAccountCannotRouteToReplacementAtOldNick(t *testing.T) {
	c, _ := newWhoxBotTestClient(t)
	c.enabledCaps["account-notify"] = true
	original, _, err := c.storage.GetOrCreatePMConversation(c.networkID, "alice", c.network.Nickname)
	if err != nil {
		t.Fatal(err)
	}
	c.handleAccount(parseLine(t, ":alice!u@h ACCOUNT alice-account"))
	resolver, ok := any(c).(interface{ ResolvePrivateTarget(string) (string, error) })
	if !ok {
		t.Fatal("IRC client cannot resolve a saved contact before sending")
	}
	target, err := resolver.ResolvePrivateTarget(storage.PMReference(original.ID))
	if err != nil || target != "alice" {
		t.Fatalf("verified recipient = %q, %v", target, err)
	}
	c.handleAccount(parseLine(t, ":alice!other@h ACCOUNT bob-account"))
	if target, err := resolver.ResolvePrivateTarget(storage.PMReference(original.ID)); err == nil {
		t.Fatalf("saved account incorrectly routed to replacement nick %q", target)
	}
}

func TestQuitWithdrawsContactSession(t *testing.T) {
	c, _ := newWhoxBotTestClient(t)
	c.enabledCaps["account-notify"] = true
	c.connected = true
	contact, _, _ := c.storage.GetOrCreatePMConversation(c.networkID, "alice", c.network.Nickname)
	c.handleAccount(parseLine(t, ":alice!u@h ACCOUNT alice-account"))
	c.handleQuit(parseLine(t, ":alice!u@h QUIT :gone"))
	state, err := c.ContactPresence(storage.PMReference(contact.ID))
	if err != nil || state != "unknown" {
		t.Fatalf("quit left a live account session: %q, %v", state, err)
	}
	if target, err := c.ResolvePrivateTarget(storage.PMReference(contact.ID)); err == nil {
		t.Fatalf("quit contact still routes to %q", target)
	}
}

func TestUntaggedLiveMessageWithdrawsAccountEvidence(t *testing.T) {
	c, _ := newWhoxBotTestClient(t)
	c.enabledCaps["account-tag"] = true
	contact, _, _ := c.storage.GetOrCreatePMConversation(c.networkID, "alice", c.network.Nickname)
	c.handlePrivmsg(parseLine(t, "@account=alice-account :alice!u@h PRIVMSG matt0x6f :identified"))
	c.handlePrivmsg(parseLine(t, ":alice!u@h PRIVMSG matt0x6f :logged out"))
	history, err := c.storage.GetPrivateMessages(c.networkID, storage.PMReference(contact.ID), c.network.Nickname, 20)
	if err != nil || len(history) != 1 {
		t.Fatalf("untagged sender inherited saved account history: %+v, %v", history, err)
	}
	if target, err := c.ResolvePrivateTarget(storage.PMReference(contact.ID)); err == nil {
		t.Fatalf("logged-out account still routes to %q", target)
	}
}

func TestCapabilityLossCannotAppendToVerifiedAccountHistory(t *testing.T) {
	c, _ := newWhoxBotTestClient(t)
	c.enabledCaps["account-tag"] = true
	contact, _, _ := c.storage.GetOrCreatePMConversation(c.networkID, "alice", c.network.Nickname)
	c.handlePrivmsg(parseLine(t, "@account=alice-account :alice!u@h PRIVMSG matt0x6f :verified"))
	c.enabledCaps["account-tag"] = false
	c.handlePrivmsg(parseLine(t, ":alice!other@h PRIVMSG matt0x6f :unverified"))
	history, err := c.storage.GetPrivateMessages(c.networkID, storage.PMReference(contact.ID), c.network.Nickname, 20)
	if err != nil || len(history) != 1 {
		t.Fatalf("disabled source still selected account history: %+v, %v", history, err)
	}
}

func TestPreferredSessionCanBeSelectedOnlyFromVerifiedContactSessions(t *testing.T) {
	c, _ := newWhoxBotTestClient(t)
	c.supportsWHOX = true
	contact, _, _ := c.storage.GetOrCreatePMConversation(c.networkID, "alice", c.network.Nickname)
	c.handleWhoxReply(parseLine(t, ":srv 354 me 332 #chan u h alice H alice-account :Alice"))
	c.handleWhoxReply(parseLine(t, ":srv 354 me 332 #chan u h ghostAlice H alice-account :Alice"))
	api, ok := any(c).(interface{ PreferPrivateContactSession(string, string) error })
	if !ok {
		t.Fatal("a contact with multiple sessions has no recipient selection")
	}
	ref := storage.PMReference(contact.ID)
	if err := api.PreferPrivateContactSession(ref, "ghostAlice"); err != nil {
		t.Fatal(err)
	}
	if target, err := c.ResolvePrivateTarget(ref); err != nil || target != "ghostAlice" {
		t.Fatalf("preferred session = %q, %v", target, err)
	}
	if err := api.PreferPrivateContactSession(ref, "stranger"); err == nil {
		t.Fatal("accepted a recipient outside the verified contact")
	}
}
