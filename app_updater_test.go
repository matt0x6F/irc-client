package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/matt0x6f/irc-client/internal/storage"
)

// newUpdaterTestApp builds a minimal App backed by a temp-file Storage, enough
// to exercise the persisted update-channel read.
func newUpdaterTestApp(t *testing.T) *App {
	t.Helper()
	dir := t.TempDir()
	s, err := storage.NewStorage(filepath.Join(dir, "updater-test.db"), 100, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return &App{storage: s, dataDir: dir}
}

func TestUpdateChannelPrerelease(t *testing.T) {
	// A nil storage (e.g. before NewApp wires it) must default to stable, never panic.
	t.Run("nil storage defaults to stable", func(t *testing.T) {
		a := &App{}
		if a.updateChannelPrerelease() {
			t.Fatal("nil storage should report stable (false)")
		}
	})

	cases := []struct {
		name  string
		value string // "" means leave the setting unset
		want  bool
	}{
		{"unset defaults to stable", "", false},
		{"explicit stable", "stable", false},
		{"unknown value defaults to stable", "garbage", false},
		{"prerelease opts in", "prerelease", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := newUpdaterTestApp(t)
			if tc.value != "" {
				// Write through storage directly to avoid the SetSetting event emit,
				// which needs a running Wails app the test doesn't construct.
				if err := a.storage.SetSetting("updateChannel", tc.value); err != nil {
					t.Fatalf("SetSetting: %v", err)
				}
			}
			if got := a.updateChannelPrerelease(); got != tc.want {
				t.Fatalf("updateChannelPrerelease() = %v, want %v", got, tc.want)
			}
		})
	}
}
