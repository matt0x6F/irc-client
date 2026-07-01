package plugin

import (
	"os"
	"path/filepath"
	"testing"
)

// PATH-based plugin discovery is a supply-chain risk (any cascade-* on PATH
// auto-loads), so it must be opt-in. With the flag off, a cascade- executable
// sitting in a PATH directory must NOT be discovered; with it on, it is.
func TestDiscoverPluginsPATHGatedByFlag(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "cascade-evil")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	off, err := DiscoverPlugins("", false)
	if err != nil {
		t.Fatalf("allowPATH=false: unexpected error: %v", err)
	}
	if len(off) != 0 {
		t.Errorf("allowPATH=false: expected no plugins, got %d (%v)", len(off), off)
	}

	on, err := DiscoverPlugins("", true)
	if err != nil {
		t.Fatalf("allowPATH=true: unexpected error: %v", err)
	}
	found := false
	for _, p := range on {
		if p.Name == "evil" {
			found = true
		}
	}
	if !found {
		t.Errorf("allowPATH=true: expected to find cascade-evil, got %v", on)
	}
}
