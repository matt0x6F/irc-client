//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsurePrivateDirTightensExistingPermissions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cascade-data")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}

	if err := ensurePrivateDir(dir); err != nil {
		t.Fatalf("ensurePrivateDir: %v", err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("data directory permissions = %04o, want 0700", got)
	}
}
