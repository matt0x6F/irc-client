package script

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCascadeSDKVersionIsV12(t *testing.T) {
	if cascadeSDKVersion != "v1.2.0" {
		t.Fatalf("cascadeSDKVersion = %q; want v1.2.0", cascadeSDKVersion)
	}
}

// reconcileModule must rewrite a stale cascade require to the app's SDK version
// while leaving the rest of a user-edited go.mod (e.g. a replace directive) intact.
func TestReconcileModuleRewritesStaleVersionPreservingEdits(t *testing.T) {
	dir := t.TempDir()
	original := "module cascade-scripts\n\ngo 1.21\n\n" +
		"require github.com/matt0x6f/irc-client/cascade v1.0.0\n\n" +
		"replace github.com/matt0x6f/irc-client/cascade => /home/me/dev/cascade\n"
	writeGoMod(t, dir, original)

	m := &Manager{dir: dir}
	if err := m.reconcileModule(); err != nil {
		t.Fatalf("reconcileModule: %v", err)
	}

	s := readGoMod(t, dir)
	if !strings.Contains(s, "github.com/matt0x6f/irc-client/cascade "+cascadeSDKVersion) {
		t.Fatalf("cascade require not updated to %s:\n%s", cascadeSDKVersion, s)
	}
	if strings.Contains(s, "cascade v1.0.0") {
		t.Fatalf("stale v1.0.0 still present:\n%s", s)
	}
	if !strings.Contains(s, "replace github.com/matt0x6f/irc-client/cascade => /home/me/dev/cascade") {
		t.Fatalf("user replace directive was clobbered:\n%s", s)
	}
}

// When the pin already matches the app's SDK, the file is left byte-for-byte
// untouched (no mtime churn for the file watcher on a steady-state launch).
func TestReconcileModuleIdempotentWhenAlreadyCurrent(t *testing.T) {
	dir := t.TempDir()
	current := "module cascade-scripts\n\ngo 1.21\n\nrequire " + cascadeModulePath + " " + cascadeSDKVersion + "\n"
	writeGoMod(t, dir, current)

	m := &Manager{dir: dir}
	if err := m.reconcileModule(); err != nil {
		t.Fatalf("reconcileModule: %v", err)
	}

	if got := readGoMod(t, dir); got != current {
		t.Fatalf("file rewritten when already current:\nwant:\n%q\ngot:\n%q", current, got)
	}
}

// A go.mod missing the cascade require entirely gets it added back.
func TestReconcileModuleAddsMissingCascadeRequire(t *testing.T) {
	dir := t.TempDir()
	writeGoMod(t, dir, "module cascade-scripts\n\ngo 1.21\n")

	m := &Manager{dir: dir}
	if err := m.reconcileModule(); err != nil {
		t.Fatalf("reconcileModule: %v", err)
	}

	if s := readGoMod(t, dir); !strings.Contains(s, cascadeModulePath+" "+cascadeSDKVersion) {
		t.Fatalf("cascade require not added:\n%s", s)
	}
}

// With no go.mod present, one is scaffolded at the app's SDK version.
func TestReconcileModuleScaffoldsWhenAbsent(t *testing.T) {
	dir := t.TempDir()

	m := &Manager{dir: dir}
	if err := m.reconcileModule(); err != nil {
		t.Fatalf("reconcileModule: %v", err)
	}

	s := readGoMod(t, dir)
	if !strings.Contains(s, "module cascade-scripts") || !strings.Contains(s, cascadeModulePath+" "+cascadeSDKVersion) {
		t.Fatalf("scaffolded go.mod missing expected content:\n%s", s)
	}
}

func writeGoMod(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(content), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
}

func readGoMod(t *testing.T, dir string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	return string(b)
}
