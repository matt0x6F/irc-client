package main

import (
	"testing"

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
		"channel.names.complete",
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
