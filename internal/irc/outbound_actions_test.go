package irc

import "testing"

func TestOutboundActionHelpersRejectDisconnectedClient(t *testing.T) {
	c := &IRCClient{}
	for name, call := range map[string]func() error{
		"notice": func() error { return c.SendNotice("alice", "hello") },
		"action": func() error { return c.SendAction("#go", "waves") },
		"part":   func() error { return c.PartChannelWithReason("#go", "bye") },
	} {
		t.Run(name, func(t *testing.T) {
			if err := call(); err == nil {
				t.Fatal("disconnected action returned nil")
			}
		})
	}
}
