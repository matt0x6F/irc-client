package main

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/matt0x6f/irc-client/internal/storage"
	"github.com/wailsapp/wails/v3/pkg/updater"
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

// TestUpdaterAccentCSSPrimaryHoverFix guards the workaround for a framework bug:
// the built-in updater window's base ".u__btn:hover" sets background to
// var(--surface-2) (near-white), and the primary button's own :hover rule only
// sets `filter` — so on hover the "Restart & Apply" button renders white
// --accent-fg text on a near-white surface and the label vanishes. Our injected
// CSS must re-assert the accent background on primary hover. The rule has to
// keep the accent background (not surface-2) so the white label stays legible.
func TestUpdaterAccentCSSPrimaryHoverFix(t *testing.T) {
	a := newUpdaterTestApp(t)
	css := a.updaterAccentCSS()
	if !strings.Contains(css, ".u__btn--primary:hover") {
		t.Fatalf("accent CSS must override the primary button hover background; got:\n%s", css)
	}
	// Pull out the primary-hover rule body and assert it keeps the accent
	// background rather than inheriting the framework's surface-2.
	idx := strings.Index(css, ".u__btn--primary:hover")
	rule := css[idx:]
	if end := strings.Index(rule, "}"); end >= 0 {
		rule = rule[:end]
	}
	if !strings.Contains(rule, "background") || !strings.Contains(rule, "var(--accent)") {
		t.Fatalf("primary-hover rule must set background to var(--accent); got: %q", rule)
	}
}

// fakeProvider is a minimal updater.Provider that records which methods ran, so
// the routing-wrapper test can assert which underlying provider was invoked
// without touching the network. Check stamps the release Version with the
// provider's name so the caller can tell them apart.
type fakeProvider struct {
	name      string
	checks    int
	downloads int
}

func (f *fakeProvider) Name() string { return f.name }

func (f *fakeProvider) Check(_ context.Context, _ updater.CheckRequest) (*updater.Release, error) {
	f.checks++
	return &updater.Release{Version: f.name}, nil
}

func (f *fakeProvider) Download(_ context.Context, _ *updater.Release, _ io.Writer, _ func(written, total int64)) error {
	f.downloads++
	return nil
}

// TestChannelRoutingProvider proves a channel switch takes effect on the very
// next Check/Download with no provider rebuild — the fix for the updater
// ignoring a live "Prerelease" selection until restart.
func TestChannelRoutingProvider(t *testing.T) {
	stable := &fakeProvider{name: "stable"}
	pre := &fakeProvider{name: "prerelease"}
	usePre := false
	p := &channelRoutingProvider{
		stable:        stable,
		prerelease:    pre,
		usePrerelease: func() bool { return usePre },
	}

	// Name must be stable so the Updater routes a follow-up Download back here.
	if p.Name() != "github" {
		t.Fatalf("Name() = %q, want github", p.Name())
	}

	// Stable channel routes Check to the stable provider.
	rel, err := p.Check(context.Background(), updater.CheckRequest{})
	if err != nil {
		t.Fatalf("Check (stable): %v", err)
	}
	if rel.Version != "stable" {
		t.Fatalf("stable channel routed to %q, want stable", rel.Version)
	}

	// Flipping the live setting reroutes the next call — no rebuild needed.
	usePre = true
	rel, err = p.Check(context.Background(), updater.CheckRequest{})
	if err != nil {
		t.Fatalf("Check (prerelease): %v", err)
	}
	if rel.Version != "prerelease" {
		t.Fatalf("after flip routed to %q, want prerelease", rel.Version)
	}
	if stable.checks != 1 || pre.checks != 1 {
		t.Fatalf("checks: stable=%d pre=%d, want 1 and 1", stable.checks, pre.checks)
	}

	// Download follows the same live setting.
	if err := p.Download(context.Background(), rel, io.Discard, func(int64, int64) {}); err != nil {
		t.Fatalf("Download: %v", err)
	}
	if pre.downloads != 1 || stable.downloads != 0 {
		t.Fatalf("downloads: stable=%d pre=%d, want 0 and 1", stable.downloads, pre.downloads)
	}
}
