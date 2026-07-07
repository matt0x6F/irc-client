package main

import (
	"testing"

	"github.com/matt0x6f/irc-client/internal/storage"
)

func makeAppTestNetwork(t *testing.T, s *storage.Storage, name string) *storage.Network {
	t.Helper()
	net := &storage.Network{Name: name, Address: "irc.example.com", Nickname: "matt"}
	if err := s.CreateNetwork(net); err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}
	return net
}
