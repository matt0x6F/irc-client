package main

import "testing"

func TestCommandRegistryRegisterAndLookup(t *testing.T) {
	r := NewCommandRegistry()
	spec := &CommandSpec{Name: "JOIN", Aliases: []string{"J"}, Category: CategoryServer, Usage: "#channel [key]", Description: "Join a channel", MinArgs: 1}
	r.Register(spec)

	for _, key := range []string{"JOIN", "join", "J", "j"} {
		got, ok := r.Lookup(key)
		if !ok || got != spec {
			t.Fatalf("Lookup(%q) = %v, %v; want the JOIN spec", key, got, ok)
		}
	}
	if _, ok := r.Lookup("NOPE"); ok {
		t.Fatalf("Lookup(NOPE) should miss")
	}
	if got := r.Specs(); len(got) != 1 {
		t.Fatalf("Specs() returned %d specs; want 1 canonical spec", len(got))
	}
}

func TestCommandRegistryDuplicatePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatalf("registering a duplicate key should panic")
		}
	}()
	r := NewCommandRegistry()
	r.Register(&CommandSpec{Name: "JOIN"})
	r.Register(&CommandSpec{Name: "JOIN"})
}
