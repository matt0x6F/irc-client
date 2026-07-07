package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/matt0x6f/irc-client/internal/events"
	"github.com/matt0x6f/irc-client/internal/storage"
)

// newTestApp builds a minimal App backed by a real temp-file Storage, enough
// to exercise the invite handler paths (GetSetting, GetMonitoredNicks).
func newTestApp(t *testing.T) *App {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "invite-test.db")
	s, err := storage.NewStorage(dbPath, 100, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return &App{storage: s}
}

func TestHandleInviteReceived_WritesActivityRow(t *testing.T) {
	a := newTestApp(t)
	net := makeAppTestNetwork(t, a.storage, "InvApp")
	var emitted []string
	a.emitFn = func(name string, data ...any) { emitted = append(emitted, name) }

	a.handleInviteReceived(events.Event{
		Type: "invite.received",
		Data: map[string]interface{}{"networkId": net.ID, "inviter": "alice", "channel": "#chan"},
	})

	invites, err := a.storage.ListInviteActivity(net.ID, time.Now())
	if err != nil {
		t.Fatalf("ListInviteActivity: %v", err)
	}
	if len(invites) != 1 || invites[0].Actor != "alice" || invites[0].Target != "#chan" {
		t.Fatalf("expected one invite row, got %+v", invites)
	}
	if invites[0].ExpiresAt == nil {
		t.Fatalf("invite should carry a TTL expiry")
	}
	assertContains(t, emitted, "activity-changed")
	assertContains(t, emitted, "invites.changed")
}

func TestInviteNotifyDecision(t *testing.T) {
	for _, level := range []string{"", "trusted", "quiet", "all"} {
		for _, enabled := range []bool{true, false} {
			for _, trusted := range []bool{true, false} {
				var want bool
				switch {
				case !enabled:
					want = false
				case level == "quiet":
					want = false
				case level == "all":
					want = true
				default: // "trusted" and unset
					want = trusted
				}
				got := inviteNotifyDecision(level, enabled, trusted)
				if got != want {
					t.Errorf("inviteNotifyDecision(%q, enabled=%v, trusted=%v) = %v, want %v",
						level, enabled, trusted, got, want)
				}
			}
		}
	}
}

func TestIgnoreActivitySender_BlocksAndRemoves(t *testing.T) {
	a := newTestApp(t)
	net := makeAppTestNetwork(t, a.storage, "InvIgn")
	a.handleInviteReceived(events.Event{Type: "invite.received", Data: map[string]interface{}{"networkId": net.ID, "inviter": "spammer", "channel": "#x"}})

	if err := a.IgnoreActivitySender(net.ID, "spammer"); err != nil {
		t.Fatalf("IgnoreActivitySender: %v", err)
	}
	if inv, _ := a.storage.ListInviteActivity(net.ID, time.Now()); len(inv) != 0 {
		t.Fatalf("ignore should remove existing invites, got %+v", inv)
	}
	// a subsequent invite from the ignored sender must not be stored
	a.handleInviteReceived(events.Event{Type: "invite.received", Data: map[string]interface{}{"networkId": net.ID, "inviter": "spammer", "channel": "#y"}})
	if inv, _ := a.storage.ListInviteActivity(net.ID, time.Now()); len(inv) != 0 {
		t.Fatalf("ignored sender's future invites must be dropped, got %+v", inv)
	}
}

func TestGetInvites_MapsFromActivity(t *testing.T) {
	a := newTestApp(t)
	net := makeAppTestNetwork(t, a.storage, "InvGet")
	a.handleInviteReceived(events.Event{Type: "invite.received", Data: map[string]interface{}{"networkId": net.ID, "inviter": "carol", "channel": "#ops"}})

	views, err := a.GetInvites(net.ID)
	if err != nil {
		t.Fatalf("GetInvites: %v", err)
	}
	if len(views) != 1 || views[0].Inviter != "carol" || views[0].Channel != "#ops" {
		t.Fatalf("GetInvites mapping wrong: %+v", views)
	}
}

func TestHandleInviteReceived_RespectsInvitesToggle(t *testing.T) {
	a := newTestApp(t)
	net := makeAppTestNetwork(t, a.storage, "InvToggleOff")
	if err := a.SetActivitySettings(ActivitySettings{Highlights: true, Keywords: true, Invites: false, PMs: true, KeywordList: []string{}}); err != nil {
		t.Fatalf("SetActivitySettings: %v", err)
	}

	a.handleInviteReceived(events.Event{
		Type: "invite.received",
		Data: map[string]interface{}{"networkId": net.ID, "inviter": "alice", "channel": "#chan"},
	})

	invites, err := a.storage.ListInviteActivity(net.ID, time.Now())
	if err != nil {
		t.Fatalf("ListInviteActivity: %v", err)
	}
	if len(invites) != 0 {
		t.Fatalf("Invites source off should skip storing the invite, got %+v", invites)
	}
}

func assertContains(t *testing.T, xs []string, want string) {
	t.Helper()
	for _, x := range xs {
		if x == want {
			return
		}
	}
	t.Fatalf("expected %q in %v", want, xs)
}
