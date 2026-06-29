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

func TestApp_HandleInviteReceived_StoresAndEmits(t *testing.T) {
	a := newTestApp(t)
	a.invites = newInviteStore(time.Now, func() time.Duration { return 24 * time.Hour })

	var emitted string
	a.emitFn = func(name string, data ...any) { emitted = name }

	a.handleInviteReceived(events.Event{
		Type: "invite.received",
		Data: map[string]interface{}{
			"networkId":  int64(1),
			"inviter":    "alice",
			"channel":    "#chan",
			"receivedAt": time.Now().Format(time.RFC3339),
		},
	})

	got, err := a.GetInvites(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Channel != "#chan" {
		t.Fatalf("expected stored invite, got %+v", got)
	}
	if emitted != "invites.changed" {
		t.Fatalf("expected invites.changed emit, got %q", emitted)
	}
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

func TestApp_IgnoreInviteSender_DropsAndBlocks(t *testing.T) {
	a := newTestApp(t)
	a.invites = newInviteStore(time.Now, func() time.Duration { return 24 * time.Hour })
	a.invites.add(1, "troll", "#x", false)

	if err := a.IgnoreInviteSender(1, "troll"); err != nil {
		t.Fatal(err)
	}
	got, _ := a.GetInvites(1)
	if len(got) != 0 {
		t.Fatalf("expected sender's invites removed, got %+v", got)
	}
}
