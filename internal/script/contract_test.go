package script

import (
	"testing"

	"github.com/matt0x6f/irc-client/internal/irc"
)

func TestEventStringContract(t *testing.T) {
	if eventMessageReceived != irc.EventMessageReceived {
		t.Fatalf("eventMessageReceived %q != irc.EventMessageReceived %q", eventMessageReceived, irc.EventMessageReceived)
	}
	if eventUserJoined != irc.EventUserJoined {
		t.Fatalf("eventUserJoined %q != irc.EventUserJoined %q", eventUserJoined, irc.EventUserJoined)
	}
	if eventUserParted != irc.EventUserParted {
		t.Fatalf("eventUserParted %q != irc.EventUserParted %q", eventUserParted, irc.EventUserParted)
	}
}
