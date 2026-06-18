package logger

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

// readFile returns the file contents, or "" if it doesn't exist.
func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func TestConfigureWritesToFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "cascade.log")
	if err := Configure(Options{FileEnabled: true, FilePath: path, Level: "info"}); err != nil {
		t.Fatalf("Configure: %v", err)
	}
	t.Cleanup(func() { _ = Close() })

	Log.Info().Msg("marker-line-present")

	if got := readFile(t, path); !strings.Contains(got, "marker-line-present") {
		t.Fatalf("log file missing message; contents=%q", got)
	}
}

func TestConfigureCreatesParentDirs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a", "b", "c", "cascade.log")
	if err := Configure(Options{FileEnabled: true, FilePath: path, Level: "info"}); err != nil {
		t.Fatalf("Configure should create parent dirs: %v", err)
	}
	t.Cleanup(func() { _ = Close() })

	Log.Info().Msg("x")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected log file at %s: %v", path, err)
	}
}

func TestLevelFiltering(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cascade.log")
	if err := Configure(Options{FileEnabled: true, FilePath: path, Level: "warn"}); err != nil {
		t.Fatalf("Configure: %v", err)
	}
	t.Cleanup(func() { _ = Close() })

	Log.Info().Msg("info-should-be-hidden")
	Log.Warn().Msg("warn-should-appear")

	got := readFile(t, path)
	if strings.Contains(got, "info-should-be-hidden") {
		t.Errorf("info line leaked at warn level; contents=%q", got)
	}
	if !strings.Contains(got, "warn-should-appear") {
		t.Errorf("warn line missing; contents=%q", got)
	}
}

func TestReconfigureDisablesFile(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.log")
	if err := Configure(Options{FileEnabled: true, FilePath: first, Level: "info"}); err != nil {
		t.Fatalf("Configure first: %v", err)
	}
	Log.Info().Msg("before-disable")

	// Disable file logging entirely.
	if err := Configure(Options{FileEnabled: false, Level: "info"}); err != nil {
		t.Fatalf("Configure disable: %v", err)
	}
	t.Cleanup(func() { _ = Close() })

	before := readFile(t, first)
	Log.Info().Msg("after-disable-should-not-land-here")
	after := readFile(t, first)

	if before != after {
		t.Errorf("file grew after disabling file logging:\nbefore=%q\nafter=%q", before, after)
	}
	if !strings.Contains(before, "before-disable") {
		t.Errorf("first file missing pre-disable line; contents=%q", before)
	}
}

func TestConfigureEnabledWithoutPathErrors(t *testing.T) {
	if err := Configure(Options{FileEnabled: true, FilePath: "  ", Level: "info"}); err == nil {
		t.Fatal("expected error when file logging enabled with blank path")
		_ = Close()
	}
}

func TestParseLevel(t *testing.T) {
	cases := map[string]zerolog.Level{
		"debug":   zerolog.DebugLevel,
		"DEBUG":   zerolog.DebugLevel,
		" info ":  zerolog.InfoLevel,
		"warn":    zerolog.WarnLevel,
		"warning": zerolog.WarnLevel,
		"error":   zerolog.ErrorLevel,
		"":        zerolog.InfoLevel,
		"bogus":   zerolog.InfoLevel,
	}
	for in, want := range cases {
		if got := ParseLevel(in); got != want {
			t.Errorf("ParseLevel(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestExpandHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home dir: %v", err)
	}
	if got := expandHome("~/logs/x.log"); got != filepath.Join(home, "logs", "x.log") {
		t.Errorf("expandHome(~/logs/x.log) = %q", got)
	}
	if got := expandHome("/abs/path.log"); got != "/abs/path.log" {
		t.Errorf("expandHome left absolute path unchanged: %q", got)
	}
}
