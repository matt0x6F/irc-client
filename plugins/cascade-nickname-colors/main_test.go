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

func TestNicknameColorParamsCanBeBatched(t *testing.T) {
	update := nicknameColorParams("Alice", float64(7))
	if update["type"] != "nickname_color" {
		t.Fatalf("type = %#v", update["type"])
	}
	if update["key"] != "nickname:Alice" {
		t.Fatalf("key = %#v", update["key"])
	}
	if update["network_id"] != int64(7) {
		t.Fatalf("network_id = %#v", update["network_id"])
	}
	if update["value"] != getColorForNickname("Alice") {
		t.Fatalf("value = %#v", update["value"])
	}
}
