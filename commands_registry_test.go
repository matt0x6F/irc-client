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

func TestBuiltinRegistryCoverage(t *testing.T) {
	r := buildBuiltinRegistry()
	// Spot-check representative commands across categories + aliases.
	cases := map[string]CommandCategory{
		"JOIN": CategoryServer, "J": CategoryServer,
		"MSG": CategoryServer, "PRIVMSG": CategoryServer, "M": CategoryServer,
		"QUERY": CategoryClient, "Q": CategoryClient,
		"CLOSE": CategoryClient, "QUOTE": CategoryServer, "RAW": CategoryServer,
		"VERSION": CategoryCTCP, "CTCP": CategoryCTCP, "PING": CategoryCTCP,
		"OP": CategoryServer, "HOP": CategoryServer, "DEVOICE": CategoryServer,
		"HELP": CategoryClient,
	}
	for name, cat := range cases {
		spec, ok := r.Lookup(name)
		if !ok {
			t.Errorf("missing built-in %q", name)
			continue
		}
		if spec.Category != cat {
			t.Errorf("%q category = %q; want %q", name, spec.Category, cat)
		}
	}
	if spec, _ := r.Lookup("HELP"); spec == nil || !spec.Frontend {
		t.Errorf("HELP must be a frontend-handled command")
	}
	if spec, _ := r.Lookup("JOIN"); spec.Usage != "#channel [key]" || spec.MinArgs != 1 {
		t.Errorf("JOIN spec metadata wrong: %+v", spec)
	}
}

func TestGetCommandsMetadata(t *testing.T) {
	a := &App{commands: buildBuiltinRegistry()}
	infos := a.GetCommands()
	if len(infos) == 0 {
		t.Fatal("GetCommands returned nothing")
	}
	var join *CommandInfo
	for i := range infos {
		if infos[i].Name == "JOIN" {
			join = &infos[i]
		}
	}
	if join == nil {
		t.Fatal("JOIN missing from GetCommands")
	}
	if join.Category != "server" || join.Usage != "#channel [key]" || len(join.Aliases) != 1 || join.Aliases[0] != "J" {
		t.Fatalf("JOIN info wrong: %+v", join)
	}
}
