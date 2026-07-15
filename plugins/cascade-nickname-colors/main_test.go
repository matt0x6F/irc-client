package main

import "testing"

func TestNicknameColorEventsExcludeUnrelatedStartupFlood(t *testing.T) {
	want := []string{
		"message.received",
		"user.joined",
		"channel.names.complete",
		"user.nick",
	}

	if len(nicknameColorEvents) != len(want) {
		t.Fatalf("nicknameColorEvents = %v, want %v", nicknameColorEvents, want)
	}
	for i := range want {
		if nicknameColorEvents[i] != want[i] {
			t.Fatalf("nicknameColorEvents = %v, want %v", nicknameColorEvents, want)
		}
	}
}
