package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/matt0x6f/irc-client/internal/logger"
	"github.com/matt0x6f/irc-client/internal/storage"
)

// newLogTestApp builds a minimal App backed by a temp-file Storage and a temp
// data dir, enough to exercise the log-config methods.
func newLogTestApp(t *testing.T) *App {
	t.Helper()
	dir := t.TempDir()
	s, err := storage.NewStorage(filepath.Join(dir, "log-test.db"), 100, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	t.Cleanup(func() { _ = logger.Close() })
	return &App{storage: s, dataDir: dir}
}

func TestGetLogConfigDefaults(t *testing.T) {
	a := newLogTestApp(t)
	cfg, err := a.GetLogConfig()
	if err != nil {
		t.Fatalf("GetLogConfig: %v", err)
	}
	if cfg.Enabled {
		t.Errorf("expected file logging disabled by default")
	}
	if cfg.Level != "info" {
		t.Errorf("expected default level info, got %q", cfg.Level)
	}
	if want := defaultLogPath(a.dataDir); cfg.Path != want {
		t.Errorf("expected default path %q, got %q", want, cfg.Path)
	}
}

func TestSetLogConfigRoundTrip(t *testing.T) {
	a := newLogTestApp(t)
	path := filepath.Join(t.TempDir(), "custom", "app.log")

	if err := a.SetLogConfig(true, path, "debug"); err != nil {
		t.Fatalf("SetLogConfig: %v", err)
	}

	cfg, err := a.GetLogConfig()
	if err != nil {
		t.Fatalf("GetLogConfig: %v", err)
	}
	if !cfg.Enabled || cfg.Path != path || cfg.Level != "debug" {
		t.Errorf("round-trip mismatch: got %+v", cfg)
	}
}

func TestSetLogConfigBlankPathFallsBackToDefault(t *testing.T) {
	a := newLogTestApp(t)
	if err := a.SetLogConfig(true, "   ", "warn"); err != nil {
		t.Fatalf("SetLogConfig: %v", err)
	}
	cfg, err := a.GetLogConfig()
	if err != nil {
		t.Fatalf("GetLogConfig: %v", err)
	}
	if cfg.Path != defaultLogPath(a.dataDir) {
		t.Errorf("expected default path for blank input, got %q", cfg.Path)
	}
	if cfg.Level != "warn" {
		t.Errorf("expected level warn, got %q", cfg.Level)
	}
}

func TestSetLogConfigNormalizesUnknownLevel(t *testing.T) {
	a := newLogTestApp(t)
	if err := a.SetLogConfig(false, "", "verbose"); err != nil {
		t.Fatalf("SetLogConfig: %v", err)
	}
	cfg, _ := a.GetLogConfig()
	if cfg.Level != "info" {
		t.Errorf("expected unknown level to normalize to info, got %q", cfg.Level)
	}
}
