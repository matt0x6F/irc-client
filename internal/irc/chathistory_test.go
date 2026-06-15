package irc

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/ergochat/irc-go/ircevent"
	"github.com/ergochat/irc-go/ircmsg"
	"github.com/matt0x6f/irc-client/internal/events"
	"github.com/matt0x6f/irc-client/internal/storage"
)

// newHistoryTestClient builds a real IRCClient backed by temp-file storage, with a
// network nicknamed "matt0x6f" and a #hist channel that exists for routing lookups.
func newHistoryTestClient(t *testing.T) *IRCClient {
	t.Helper()
	dir := t.TempDir()
	s, err := storage.NewStorage(filepath.Join(dir, "test.db"), 100, 20*time.Millisecond)
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	net := &storage.Network{Name: "Ergo", Address: "irc.example.com", Nickname: "matt0x6f", CreatedAt: time.Now()}
	if err := s.CreateNetwork(net); err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}
	if err := s.CreateChannel(&storage.Channel{NetworkID: net.ID, Name: "#hist", CreatedAt: time.Now()}); err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}
	return &IRCClient{
		eventBus:    events.NewEventBus(),
		storage:     s,
		networkID:   net.ID,
		network:     net,
		enabledCaps: map[string]bool{"chathistory": true, "batch": true, "server-time": true, "message-tags": true},
	}
}

// historyEventSink captures EventHistoryReceived events for assertions.
type historyEventSink struct{ ch chan events.Event }

func (h *historyEventSink) OnEvent(e events.Event) { h.ch <- e }

// mustParseBatchItem parses a raw IRC line into a *ircevent.Batch item.
func mustParseBatchItem(t *testing.T, raw string) *ircevent.Batch {
	t.Helper()
	m, err := ircmsg.ParseLine(raw)
	if err != nil {
		t.Fatalf("ParseLine(%q): %v", raw, err)
	}
	return &ircevent.Batch{Message: m}
}

// TestHandleChatHistoryBatchChannel feeds a synthetic CHATHISTORY batch for a
// channel through the real handler and asserts the replayed lines are persisted
// with the right routing, server-time timestamps, and msgid-based dedup, and that
// a single history event is emitted.
func TestHandleChatHistoryBatchChannel(t *testing.T) {
	c := newHistoryTestClient(t)

	sink := &historyEventSink{ch: make(chan events.Event, 4)}
	c.eventBus.Subscribe(EventHistoryReceived, sink)

	start, err := ircmsg.ParseLine("BATCH +1 chathistory #hist")
	if err != nil {
		t.Fatalf("ParseLine(batch start): %v", err)
	}
	batch := &ircevent.Batch{
		Message: start,
		Items: []*ircevent.Batch{
			mustParseBatchItem(t, "@time=2024-06-14T10:00:00.000Z;msgid=m1 :alice!a@h PRIVMSG #hist :first"),
			mustParseBatchItem(t, "@time=2024-06-14T10:01:00.000Z;msgid=m2 :bob!b@h PRIVMSG #hist :second"),
		},
	}

	if claimed := c.handleChatHistoryBatch(batch); !claimed {
		t.Fatal("expected handler to claim the chathistory batch (return true)")
	}

	ch, err := c.storage.GetChannelByName(c.networkID, "#hist")
	if err != nil {
		t.Fatalf("GetChannelByName: %v", err)
	}
	msgs, err := c.storage.GetMessages(c.networkID, &ch.ID, 50)
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 stored history messages, got %d", len(msgs))
	}
	// Server-time was parsed from @time (not receive-time).
	wantTime := time.Date(2024, 6, 14, 10, 0, 0, 0, time.UTC)
	if !msgs[0].Timestamp.Equal(wantTime) {
		t.Errorf("expected first message timestamp %v, got %v", wantTime, msgs[0].Timestamp)
	}
	if msgs[0].MsgID != "m1" {
		t.Errorf("expected msgid m1, got %q", msgs[0].MsgID)
	}

	// A single history event should have been emitted with inserted=2.
	select {
	case e := <-sink.ch:
		if got, _ := e.Data["inserted"].(int); got != 2 {
			t.Errorf("expected inserted=2 in history event, got %v", e.Data["inserted"])
		}
		if got, _ := e.Data["target"].(string); got != "#hist" {
			t.Errorf("expected target #hist, got %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for history event")
	}

	// Replaying the same batch must insert nothing (msgid dedup).
	if claimed := c.handleChatHistoryBatch(batch); !claimed {
		t.Fatal("expected handler to claim the replayed batch")
	}
	msgs2, err := c.storage.GetMessages(c.networkID, &ch.ID, 50)
	if err != nil {
		t.Fatalf("GetMessages (after replay): %v", err)
	}
	if len(msgs2) != 2 {
		t.Fatalf("expected dedup to keep 2 messages, got %d", len(msgs2))
	}
}

// TestHandleChatHistoryBatchPM verifies PM-targeted history is routed to the
// per-peer query pane (pm_target), mirroring the live PRIVMSG handler.
func TestHandleChatHistoryBatchPM(t *testing.T) {
	c := newHistoryTestClient(t)

	start, err := ircmsg.ParseLine("BATCH +1 chathistory carol")
	if err != nil {
		t.Fatalf("ParseLine(batch start): %v", err)
	}
	batch := &ircevent.Batch{
		Message: start,
		Items: []*ircevent.Batch{
			mustParseBatchItem(t, "@time=2024-06-14T09:00:00.000Z;msgid=p1 :carol!c@h PRIVMSG matt0x6f :hi matt"),
		},
	}

	if claimed := c.handleChatHistoryBatch(batch); !claimed {
		t.Fatal("expected handler to claim the PM chathistory batch")
	}

	msgs, err := c.storage.GetPrivateMessages(c.networkID, "carol", "matt0x6f", 50)
	if err != nil {
		t.Fatalf("GetPrivateMessages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 PM history message, got %d", len(msgs))
	}
	if msgs[0].Message != "hi matt" {
		t.Errorf("unexpected PM message: %q", msgs[0].Message)
	}
}

// TestHandleChatHistoryBatchIgnoresOtherTypes verifies non-chathistory batches are
// not claimed (returned false), so the library keeps handling them as before.
func TestHandleChatHistoryBatchIgnoresOtherTypes(t *testing.T) {
	c := newHistoryTestClient(t)

	start, err := ircmsg.ParseLine("BATCH +1 netjoin")
	if err != nil {
		t.Fatalf("ParseLine: %v", err)
	}
	batch := &ircevent.Batch{Message: start}
	if claimed := c.handleChatHistoryBatch(batch); claimed {
		t.Fatal("expected handler to ignore a non-chathistory batch (return false)")
	}
}
