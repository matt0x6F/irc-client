package storage

import (
	"reflect"
	"testing"
)

func TestMonitoredNicksRoundTrip(t *testing.T) {
	s := newTestStorage(t)
	net := makeNetwork("MonNet")
	if err := s.CreateNetwork(net); err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}

	// Empty to start.
	if got, err := s.GetMonitoredNicks(net.ID); err != nil || len(got) != 0 {
		t.Fatalf("GetMonitoredNicks(empty) = %v, err %v", got, err)
	}

	if err := s.AddMonitoredNick(net.ID, "bob"); err != nil {
		t.Fatalf("AddMonitoredNick: %v", err)
	}
	if err := s.AddMonitoredNick(net.ID, "alice"); err != nil {
		t.Fatalf("AddMonitoredNick: %v", err)
	}
	// Re-adding is a no-op (idempotent), not a duplicate or error.
	if err := s.AddMonitoredNick(net.ID, "bob"); err != nil {
		t.Fatalf("AddMonitoredNick(dup): %v", err)
	}

	got, err := s.GetMonitoredNicks(net.ID)
	if err != nil {
		t.Fatalf("GetMonitoredNicks: %v", err)
	}
	if want := []string{"alice", "bob"}; !reflect.DeepEqual(got, want) { // ORDER BY nickname
		t.Fatalf("GetMonitoredNicks = %v, want %v", got, want)
	}

	if err := s.RemoveMonitoredNick(net.ID, "bob"); err != nil {
		t.Fatalf("RemoveMonitoredNick: %v", err)
	}
	if got, _ := s.GetMonitoredNicks(net.ID); !reflect.DeepEqual(got, []string{"alice"}) {
		t.Fatalf("after remove = %v, want [alice]", got)
	}
}
