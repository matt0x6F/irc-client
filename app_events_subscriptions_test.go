package main

import (
	"testing"
	"time"

	"github.com/matt0x6f/irc-client/internal/events"
	"github.com/matt0x6f/irc-client/internal/irc"
)

// OnEvent forwards these event types to the frontend, but they were historically
// left out of the App's bus subscriptions — dead forwards the removed 2-second
// poll masked for the pollable ones (nick, topic, errors) and that never arrived
// at all for the rest (typing, history, user-meta, bot, SASL-failure). This test
// pins them into appForwardedEventTypes so a future edit can't silently drop one.
func TestAppSubscribesPreviouslyDeadForwardedEvents(t *testing.T) {
	subscribed := make(map[string]bool, len(appForwardedEventTypes))
	for _, ev := range appForwardedEventTypes {
		subscribed[ev] = true
	}

	required := []string{
		irc.EventNickChanged,
		irc.EventChannelTopic,
		irc.EventError,
		irc.EventChannelNamesComplete,
		irc.EventHistoryReceived,
		irc.EventTypingReceived,
		irc.EventBotDetected,
		irc.EventUserMetaChanged,
		irc.EventSASLFailed,
		irc.EventStatusMessage,
		irc.EventDCCControl,
	}
	for _, ev := range required {
		if !subscribed[ev] {
			t.Errorf("event %q is forwarded by OnEvent but missing from appForwardedEventTypes — dead forward", ev)
		}
	}
}

func TestNamesCompleteBatchesFrontendRefreshAfterTrailingEdge(t *testing.T) {
	emitted := make(chan struct {
		name string
		data map[string]interface{}
	}, 1)
	a := &App{emitFn: func(name string, data ...any) {
		var payload map[string]interface{}
		if len(data) == 1 {
			payload, _ = data[0].(map[string]interface{})
		}
		emitted <- struct {
			name string
			data map[string]interface{}
		}{name: name, data: payload}
	}}

	a.OnEvent(events.Event{
		Type: irc.EventChannelNamesComplete,
		Data: map[string]interface{}{
			"networkId": int64(7),
			"channel":   "#large",
			"users":     []string{"alice", "bob"},
		},
	})

	select {
	case got := <-emitted:
		if got.name != "channel-rosters-complete" {
			t.Fatalf("emitted event = %q, want channel-rosters-complete", got.name)
		}
		updates, ok := got.data["updates"].([]map[string]interface{})
		if !ok || len(updates) != 1 || updates[0]["channel"] != "#large" {
			t.Fatalf("emitted updates = %#v", got.data["updates"])
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for trailing-edge roster completion")
	}
}
