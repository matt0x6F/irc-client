package irc

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/ergochat/irc-go/ircmsg"
	"github.com/matt0x6f/irc-client/internal/events"
	"github.com/matt0x6f/irc-client/internal/storage"
)

// newNoticeTestClient builds a real IRCClient backed by a temp-file storage and
// a real event bus, with a network whose nickname matches the NOTICE targets
// used in the tests. This exercises the true production path:
// raw IRC line -> ircmsg.ParseLine -> handleNotice -> storage.
func newNoticeTestClient(t *testing.T) *IRCClient {
	t.Helper()
	dir := t.TempDir()
	s, err := storage.NewStorage(filepath.Join(dir, "test.db"), 100, 20*time.Millisecond)
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	net := &storage.Network{Name: "Libera", Address: "irc.libera.chat", Nickname: "matt0x6f", CreatedAt: time.Now()}
	if err := s.CreateNetwork(net); err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}
	return &IRCClient{
		eventBus:  events.NewEventBus(),
		storage:   s,
		networkID: net.ID,
		network:   net,
	}
}

// TestHandleNoticeRoutingEndToEnd drives the real handleNotice with raw IRC
// lines (parsed by the actual library) and asserts where each notice lands.
// This is the regression guard the unit/storage tests missed: it runs the whole
// chain that the live app runs. The ChanServ line is the exact raw line captured
// from the user's database.
func TestHandleNoticeRoutingEndToEnd(t *testing.T) {
	cases := []struct {
		name       string
		raw        string
		wantPeer   string // "" => expect it in Status (no pm_target)
		wantInChan string // non-empty => expect it in this channel buffer
	}{
		{
			name:     "real ChanServ hostmask notice (from user DB)",
			raw:      ":ChanServ!ChanServ@services.libera.chat NOTICE matt0x6f :Invalid ChanServ command.",
			wantPeer: "ChanServ",
		},
		{
			name:     "bare-source service notice",
			raw:      ":ChanServ NOTICE matt0x6f :bare-source reply",
			wantPeer: "ChanServ",
		},
		{
			name:     "NickServ hostmask notice",
			raw:      ":NickServ!NickServ@services. NOTICE matt0x6f :You are now identified for matt0x6f",
			wantPeer: "NickServ",
		},
		{
			name:     "server notice to our nick stays in Status",
			raw:      ":calcium.libera.chat NOTICE matt0x6f :*** Notice -- server message",
			wantPeer: "",
		},
		{
			name:     "pre-registration server notice to * stays in Status",
			raw:      ":calcium.libera.chat NOTICE * :*** Looking up your hostname...",
			wantPeer: "",
		},
		{
			name:       "channel notice goes to the channel buffer",
			raw:        ":bot!b@h NOTICE #chan :build passed",
			wantInChan: "#chan",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newNoticeTestClient(t)

			// For the channel case, the channel must exist for the lookup to set channel_id.
			if tc.wantInChan != "" {
				if err := c.storage.CreateChannel(&storage.Channel{NetworkID: c.networkID, Name: tc.wantInChan, CreatedAt: time.Now()}); err != nil {
					t.Fatalf("CreateChannel: %v", err)
				}
			}

			e, err := ircmsg.ParseLine(tc.raw)
			if err != nil {
				t.Fatalf("ParseLine(%q): %v", tc.raw, err)
			}
			c.handleNotice(e)

			notice := e.Params[1]

			switch {
			case tc.wantInChan != "":
				ch, err := c.storage.GetChannelByName(c.networkID, tc.wantInChan)
				if err != nil {
					t.Fatalf("GetChannelByName: %v", err)
				}
				msgs, err := c.storage.GetMessages(c.networkID, &ch.ID, 50)
				if err != nil {
					t.Fatalf("GetMessages(channel): %v", err)
				}
				if len(msgs) != 1 || msgs[0].Message != notice {
					t.Fatalf("expected the notice in %s, got %+v", tc.wantInChan, msgs)
				}

			case tc.wantPeer != "":
				// Must appear in the peer's query pane...
				pm, err := c.storage.GetPrivateMessages(c.networkID, tc.wantPeer, c.network.Nickname, 50)
				if err != nil {
					t.Fatalf("GetPrivateMessages(%s): %v", tc.wantPeer, err)
				}
				if len(pm) != 1 || pm[0].Message != notice {
					t.Fatalf("expected the notice in %s's query pane, got %+v", tc.wantPeer, pm)
				}
				// ...and NOT in Status.
				status, err := c.storage.GetMessages(c.networkID, nil, 50)
				if err != nil {
					t.Fatalf("GetMessages(status): %v", err)
				}
				if len(status) != 0 {
					t.Fatalf("notice leaked into Status: %+v", status)
				}

			default: // expect in Status
				// Server notices use the buffered write path, so poll until the
				// background flush persists the row (condition-based wait, not a
				// fixed sleep) rather than reading before the buffer flushes.
				var status []storage.Message
				deadline := time.Now().Add(2 * time.Second)
				for time.Now().Before(deadline) {
					status, err = c.storage.GetMessages(c.networkID, nil, 50)
					if err != nil {
						t.Fatalf("GetMessages(status): %v", err)
					}
					if len(status) > 0 {
						break
					}
					time.Sleep(10 * time.Millisecond)
				}
				if len(status) != 1 || status[0].Message != notice {
					t.Fatalf("expected the server notice in Status, got %+v", status)
				}
			}
		})
	}
}
