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
		eventBus:           events.NewEventBus(),
		storage:            s,
		networkID:          net.ID,
		network:            net,
		enabledCaps:        map[string]bool{"chathistory": true, "batch": true, "server-time": true, "message-tags": true},
		serverCapabilities: &ServerCapabilities{},
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

// TestBuildHistoryMessageJoin verifies a replayed JOIN in a CHATHISTORY batch is
// accepted and stored as a "join" row carrying the original msgid.
func TestBuildHistoryMessageJoin(t *testing.T) {
	c := newHistoryTestClient(t)
	e := ircmsg.MakeMessage(map[string]string{"msgid": "j1", "time": "2026-07-07T00:00:00Z"}, "alice!u@h", "JOIN", "#hist")
	msg, ok := c.buildHistoryMessage(e, "#hist")
	if !ok {
		t.Fatal("expected JOIN to be accepted")
	}
	if msg.MessageType != "join" || msg.MsgID != "j1" {
		t.Fatalf("got type=%q msgid=%q", msg.MessageType, msg.MsgID)
	}
	if msg.ChannelID == nil {
		t.Fatal("expected JOIN to resolve a channel id")
	}
}

// TestBuildHistoryMessageQuitUsesBatchTarget verifies a replayed QUIT — which
// carries no channel param on the wire — is routed to the batch's target channel.
func TestBuildHistoryMessageQuitUsesBatchTarget(t *testing.T) {
	c := newHistoryTestClient(t)
	// QUIT has no channel param; the builder must route it to the batch target.
	e := ircmsg.MakeMessage(map[string]string{"msgid": "q1"}, "bob!u@h", "QUIT", "connection closed")
	msg, ok := c.buildHistoryMessage(e, "#hist")
	if !ok || msg.MessageType != "quit" || msg.ChannelID == nil {
		t.Fatalf("quit not routed to batch target: ok=%v type=%q ch=%v", ok, msg.MessageType, msg.ChannelID)
	}
	if msg.MsgID != "q1" {
		t.Fatalf("expected msgid q1, got %q", msg.MsgID)
	}
}

// TestBuildHistoryMessagePart verifies a replayed PART becomes a "part" row.
func TestBuildHistoryMessagePart(t *testing.T) {
	c := newHistoryTestClient(t)
	e := ircmsg.MakeMessage(map[string]string{"msgid": "p1"}, "alice!u@h", "PART", "#hist", "bye")
	msg, ok := c.buildHistoryMessage(e, "#hist")
	if !ok || msg.MessageType != "part" || msg.ChannelID == nil {
		t.Fatalf("part not accepted: ok=%v type=%q ch=%v", ok, msg.MessageType, msg.ChannelID)
	}
	if msg.MsgID != "p1" {
		t.Fatalf("expected msgid p1, got %q", msg.MsgID)
	}
}

// TestBuildHistoryMessageKick verifies a replayed KICK becomes a "kick" row.
func TestBuildHistoryMessageKick(t *testing.T) {
	c := newHistoryTestClient(t)
	e := ircmsg.MakeMessage(map[string]string{"msgid": "k1"}, "op!u@h", "KICK", "#hist", "alice", "spamming")
	msg, ok := c.buildHistoryMessage(e, "#hist")
	if !ok || msg.MessageType != "kick" || msg.ChannelID == nil {
		t.Fatalf("kick not accepted: ok=%v type=%q ch=%v", ok, msg.MessageType, msg.ChannelID)
	}
	if msg.MsgID != "k1" {
		t.Fatalf("expected msgid k1, got %q", msg.MsgID)
	}
}

// TestBuildHistoryMessageMode verifies a replayed MODE becomes a "mode" row.
func TestBuildHistoryMessageMode(t *testing.T) {
	c := newHistoryTestClient(t)
	e := ircmsg.MakeMessage(map[string]string{"msgid": "md1"}, "op!u@h", "MODE", "#hist", "+o", "alice")
	msg, ok := c.buildHistoryMessage(e, "#hist")
	if !ok || msg.MessageType != "mode" || msg.ChannelID == nil {
		t.Fatalf("mode not accepted: ok=%v type=%q ch=%v", ok, msg.MessageType, msg.ChannelID)
	}
	if msg.MsgID != "md1" {
		t.Fatalf("expected msgid md1, got %q", msg.MsgID)
	}
}

// countMessagesOfType returns how many stored messages of the given
// message_type exist for the named channel.
func countMessagesOfType(t *testing.T, c *IRCClient, channelName, messageType string) int {
	t.Helper()
	ch, err := c.storage.GetChannelByName(c.networkID, channelName)
	if err != nil {
		t.Fatalf("GetChannelByName(%q): %v", channelName, err)
	}
	msgs, err := c.storage.GetMessages(c.networkID, &ch.ID, 100)
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	n := 0
	for _, m := range msgs {
		if m.MessageType == messageType {
			n++
		}
	}
	return n
}

// TestReplayedEventDedupsAgainstLive locks in the end-to-end contract that
// draft/event-playback exists to enable: a live membership event (handled by
// the live QUIT/PART/JOIN/KICK/MODE handlers, which now persist @msgid) and
// its later CHATHISTORY replay (built by buildHistoryMessage using the same
// original msgid) must collapse to a single row via the per-conversation
// (network_id, conversation, msgid) unique index — not one row per source.
//
// This is non-tautological: if the live handler failed to set MsgID, or the
// dedup index were coarser (e.g. global instead of per-conversation, or
// keyed only on channel_id without msgid), the replay would insert a second
// "quit" row and the assertions below would fail.
func TestReplayedEventDedupsAgainstLive(t *testing.T) {
	c := newHistoryTestClient(t)

	ch, err := c.storage.GetChannelByName(c.networkID, "#hist")
	if err != nil {
		t.Fatalf("GetChannelByName: %v", err)
	}
	// bob must be a member of #hist for the live QUIT handler to record a
	// quit row there (it only writes to channels the quitting user was in).
	if err := c.storage.AddChannelUser(ch.ID, "bob", ""); err != nil {
		t.Fatalf("AddChannelUser: %v", err)
	}

	// Live QUIT arrives first and is stored via the real handler.
	live := ircmsg.MakeMessage(map[string]string{"msgid": "q9"}, "bob!u@h", "QUIT", "bye")
	c.handleQuit(live)

	if got := countMessagesOfType(t, c, "#hist", "quit"); got != 1 {
		t.Fatalf("expected 1 quit row after live QUIT, got %d", got)
	}

	// Server later replays the same QUIT (same original msgid) inside a
	// CHATHISTORY batch targeting #hist; the builder must route it there.
	replayEvent := ircmsg.MakeMessage(map[string]string{"msgid": "q9"}, "bob!u@h", "QUIT", "bye")
	replay, ok := c.buildHistoryMessage(replayEvent, "#hist")
	if !ok {
		t.Fatal("builder rejected replayed quit")
	}
	if replay.MsgID != "q9" {
		t.Fatalf("expected replay to carry original msgid q9, got %q", replay.MsgID)
	}

	n, err := c.storage.WriteHistoryMessages([]storage.Message{replay})
	if err != nil {
		t.Fatalf("WriteHistoryMessages: %v", err)
	}
	if n != 0 {
		t.Fatalf("replay should dedup against the live row (0 new rows), got %d", n)
	}
	if got := countMessagesOfType(t, c, "#hist", "quit"); got != 1 {
		t.Fatalf("want exactly 1 quit row after replay, got %d", got)
	}
}

// TestReplayedKickDedupsAgainstLive mirrors TestReplayedEventDedupsAgainstLive
// for KICK: the live KICK handler must persist @msgid so a later CHATHISTORY
// replay of the same kick (same original msgid) collapses to a single row via
// the per-conversation dedup index instead of inserting a second "kick" row.
func TestReplayedKickDedupsAgainstLive(t *testing.T) {
	c := newHistoryTestClient(t)

	ch, err := c.storage.GetChannelByName(c.networkID, "#hist")
	if err != nil {
		t.Fatalf("GetChannelByName: %v", err)
	}
	// alice must be a member of #hist for the kicked-user removal path (and
	// realistic fixture) even though the kick row is written regardless.
	if err := c.storage.AddChannelUser(ch.ID, "alice", ""); err != nil {
		t.Fatalf("AddChannelUser: %v", err)
	}

	// Live KICK arrives first and is stored via the real handler.
	live := ircmsg.MakeMessage(map[string]string{"msgid": "k9"}, "op!u@h", "KICK", "#hist", "alice", "spamming")
	c.handleKick(live)

	if got := countMessagesOfType(t, c, "#hist", "kick"); got != 1 {
		t.Fatalf("expected 1 kick row after live KICK, got %d", got)
	}

	// Server later replays the same KICK (same original msgid) inside a
	// CHATHISTORY batch targeting #hist; the builder must route it there.
	replayEvent := ircmsg.MakeMessage(map[string]string{"msgid": "k9"}, "op!u@h", "KICK", "#hist", "alice", "spamming")
	replay, ok := c.buildHistoryMessage(replayEvent, "#hist")
	if !ok {
		t.Fatal("builder rejected replayed kick")
	}
	if replay.MsgID != "k9" {
		t.Fatalf("expected replay to carry original msgid k9, got %q", replay.MsgID)
	}

	n, err := c.storage.WriteHistoryMessages([]storage.Message{replay})
	if err != nil {
		t.Fatalf("WriteHistoryMessages: %v", err)
	}
	if n != 0 {
		t.Fatalf("replay should dedup against the live row (0 new rows), got %d", n)
	}
	if got := countMessagesOfType(t, c, "#hist", "kick"); got != 1 {
		t.Fatalf("want exactly 1 kick row after replay, got %d", got)
	}
}

// TestReplayedModeDedupsAgainstLive mirrors TestReplayedEventDedupsAgainstLive
// for MODE: the live MODE handler must persist @msgid so a later CHATHISTORY
// replay of the same mode change (same original msgid) collapses to a single
// row via the per-conversation dedup index instead of inserting a second
// "mode" row.
func TestReplayedModeDedupsAgainstLive(t *testing.T) {
	c := newHistoryTestClient(t)

	ch, err := c.storage.GetChannelByName(c.networkID, "#hist")
	if err != nil {
		t.Fatalf("GetChannelByName: %v", err)
	}
	// alice must already be tracked as a channel member for the per-user
	// prefix-change branch to find her current modes (not required for the
	// "sets mode" row itself, but keeps the fixture realistic).
	if err := c.storage.AddChannelUser(ch.ID, "alice", ""); err != nil {
		t.Fatalf("AddChannelUser: %v", err)
	}

	// Live MODE arrives first and is stored via the real handler.
	live := ircmsg.MakeMessage(map[string]string{"msgid": "m9"}, "op!u@h", "MODE", "#hist", "+o", "alice")
	c.handleMode(live)

	if got := countMessagesOfType(t, c, "#hist", "mode"); got != 1 {
		t.Fatalf("expected 1 mode row after live MODE, got %d", got)
	}

	// Server later replays the same MODE (same original msgid) inside a
	// CHATHISTORY batch targeting #hist; the builder must route it there.
	replayEvent := ircmsg.MakeMessage(map[string]string{"msgid": "m9"}, "op!u@h", "MODE", "#hist", "+o", "alice")
	replay, ok := c.buildHistoryMessage(replayEvent, "#hist")
	if !ok {
		t.Fatal("builder rejected replayed mode")
	}
	if replay.MsgID != "m9" {
		t.Fatalf("expected replay to carry original msgid m9, got %q", replay.MsgID)
	}

	n, err := c.storage.WriteHistoryMessages([]storage.Message{replay})
	if err != nil {
		t.Fatalf("WriteHistoryMessages: %v", err)
	}
	if n != 0 {
		t.Fatalf("replay should dedup against the live row (0 new rows), got %d", n)
	}
	if got := countMessagesOfType(t, c, "#hist", "mode"); got != 1 {
		t.Fatalf("want exactly 1 mode row after replay, got %d", got)
	}
}
