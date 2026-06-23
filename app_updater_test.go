package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/matt0x6f/irc-client/internal/storage"
	"github.com/wailsapp/wails/v3/pkg/updater"
	"github.com/wailsapp/wails/v3/pkg/updater/providers/github"
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

// TestMatchUpdateAsset covers the cross-platform release-asset picker the github
// updater provider calls. It must select the right archive per OS+arch and reject
// everything else: the wrong format, the wrong arch, sibling installers/checksums,
// and unknown platforms. Matching is case-insensitive and accepts the common arch
// aliases (x86_64/x64 for amd64, aarch64 for arm64) so a renamed asset still maps.
func TestMatchUpdateAsset(t *testing.T) {
	// A realistic full release asset set; cases index into a per-case subset.
	mac := github.ReleaseAsset{Name: "Cascade-1.2.0-universal.zip"}
	macDmg := github.ReleaseAsset{Name: "Cascade-1.2.0-universal.dmg"}
	winAmd := github.ReleaseAsset{Name: "Cascade-1.2.0-windows-amd64.zip"}
	winArm := github.ReleaseAsset{Name: "Cascade-1.2.0-windows-arm64.zip"}
	winInst := github.ReleaseAsset{Name: "cascade-amd64-installer.exe"}
	linAmd := github.ReleaseAsset{Name: "Cascade-1.2.0-linux-amd64.tar.gz"}
	linArm := github.ReleaseAsset{Name: "Cascade-1.2.0-linux-arm64.tar.gz"}
	linDeb := github.ReleaseAsset{Name: "cascade_1.2.0_amd64.deb"}
	linRpm := github.ReleaseAsset{Name: "cascade-1.2.0.x86_64.rpm"}
	linAppImage := github.ReleaseAsset{Name: "Cascade-1.2.0-amd64.AppImage"}
	sums := github.ReleaseAsset{Name: "SHA256SUMS"}

	cases := []struct {
		name     string
		platform string
		arch     string
		assets   []github.ReleaseAsset
		want     int // index into assets, or -1
	}{
		{"darwin picks universal zip", "darwin", "arm64", []github.ReleaseAsset{macDmg, mac, sums}, 1},
		{"darwin arch-agnostic (amd64 still gets universal)", "darwin", "amd64", []github.ReleaseAsset{mac, sums}, 0},
		{"darwin ignores dmg and checksums", "darwin", "amd64", []github.ReleaseAsset{macDmg, sums}, -1},

		{"windows amd64", "windows", "amd64", []github.ReleaseAsset{winArm, winAmd}, 1},
		{"windows arm64", "windows", "arm64", []github.ReleaseAsset{winAmd, winArm}, 1},
		{"windows no matching arch", "windows", "arm64", []github.ReleaseAsset{winAmd}, -1},
		{"windows ignores other-OS assets", "windows", "amd64", []github.ReleaseAsset{linAmd, mac, winAmd}, 2},
		{"windows ignores installer exe", "windows", "amd64", []github.ReleaseAsset{winInst}, -1},

		{"linux amd64", "linux", "amd64", []github.ReleaseAsset{linArm, linAmd}, 1},
		{"linux arm64", "linux", "arm64", []github.ReleaseAsset{linAmd, linArm}, 1},
		{"linux no matching arch", "linux", "arm64", []github.ReleaseAsset{linAmd}, -1},
		{"linux ignores deb rpm appimage", "linux", "amd64", []github.ReleaseAsset{linDeb, linRpm, linAppImage, linAmd}, 3},
		{"linux rejects zip (must be tar.gz)", "linux", "amd64", []github.ReleaseAsset{{Name: "Cascade-1.2.0-linux-amd64.zip"}}, -1},

		{"case-insensitive linux", "linux", "amd64", []github.ReleaseAsset{{Name: "CASCADE-1.2.0-LINUX-AMD64.TAR.GZ"}}, 0},
		{"arch alias x86_64 for amd64", "linux", "amd64", []github.ReleaseAsset{{Name: "cascade-1.2.0-linux-x86_64.tar.gz"}}, 0},
		{"arch alias aarch64 for arm64", "linux", "arm64", []github.ReleaseAsset{{Name: "cascade-1.2.0-linux-aarch64.tar.gz"}}, 0},
		{"amd64 never matches arm64 asset", "linux", "amd64", []github.ReleaseAsset{linArm}, -1},

		{"unknown platform", "freebsd", "amd64", []github.ReleaseAsset{mac, winAmd, linAmd}, -1},
		{"empty asset list", "linux", "amd64", nil, -1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := updater.CheckRequest{Platform: tc.platform, Arch: tc.arch}
			if got := matchUpdateAsset(req, tc.assets); got != tc.want {
				t.Fatalf("matchUpdateAsset(%s/%s) = %d, want %d", tc.platform, tc.arch, got, tc.want)
			}
		})
	}
}

// TestLinuxSelfUpdateEligible covers the gate that decides whether a Linux
// install can swap itself in place. Only a bare-binary install in a user-writable
// directory is eligible; AppImage (detected via $APPIMAGE or a /tmp/.mount_ path)
// and system installs (non-writable dir, e.g. deb/rpm under /usr/bin) are not, so
// the updater degrades to a "download it" prompt instead of a doomed swap.
func TestLinuxSelfUpdateEligible(t *testing.T) {
	// Restore the os.Executable seam after each subtest.
	orig := osExecutable
	t.Cleanup(func() { osExecutable = orig })

	t.Run("bare binary in writable dir is eligible", func(t *testing.T) {
		dir := t.TempDir()
		osExecutable = func() (string, error) { return filepath.Join(dir, "cascade"), nil }
		t.Setenv("APPIMAGE", "")
		ok, reason := linuxSelfUpdateEligible()
		if !ok || reason != "" {
			t.Fatalf("writable bare binary = (%v, %q), want (true, \"\")", ok, reason)
		}
	})

	t.Run("APPIMAGE env marks ineligible", func(t *testing.T) {
		dir := t.TempDir()
		osExecutable = func() (string, error) { return filepath.Join(dir, "cascade"), nil }
		t.Setenv("APPIMAGE", "/home/u/Apps/Cascade.AppImage")
		ok, reason := linuxSelfUpdateEligible()
		if ok || reason != "appimage" {
			t.Fatalf("APPIMAGE set = (%v, %q), want (false, \"appimage\")", ok, reason)
		}
	})

	t.Run("mount path marks ineligible", func(t *testing.T) {
		osExecutable = func() (string, error) { return "/tmp/.mount_Cascadexyz/usr/bin/cascade", nil }
		t.Setenv("APPIMAGE", "")
		ok, reason := linuxSelfUpdateEligible()
		if ok || reason != "appimage" {
			t.Fatalf("mount path = (%v, %q), want (false, \"appimage\")", ok, reason)
		}
	})

	t.Run("non-writable dir marks ineligible", func(t *testing.T) {
		dir := t.TempDir()
		ro := filepath.Join(dir, "ro")
		if err := os.Mkdir(ro, 0o500); err != nil {
			t.Fatalf("mkdir ro: %v", err)
		}
		t.Cleanup(func() { _ = os.Chmod(ro, 0o700) })
		osExecutable = func() (string, error) { return filepath.Join(ro, "cascade"), nil }
		t.Setenv("APPIMAGE", "")
		ok, reason := linuxSelfUpdateEligible()
		if ok || reason != "system" {
			t.Fatalf("read-only dir = (%v, %q), want (false, \"system\")", ok, reason)
		}
	})
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

// TestShouldSurfaceUpdate proves the background-check gate suppresses exactly
// the version the user skipped and nothing else — a newer (or any different)
// version still surfaces, and an empty found version never surfaces.
func TestShouldSurfaceUpdate(t *testing.T) {
	cases := []struct {
		name    string
		found   string
		skipped string
		want    bool
	}{
		{"no update found", "", "", false},
		{"no update found even if a version was skipped", "", "26.6.6-rc.51", false},
		{"found, nothing skipped", "26.6.6-rc.51", "", true},
		{"found equals skipped — suppressed", "26.6.6-rc.51", "26.6.6-rc.51", false},
		{"newer than skipped still surfaces", "26.6.7", "26.6.6-rc.51", true},
		{"different older still surfaces (skip is exact-match)", "26.6.5", "26.6.6-rc.51", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldSurfaceUpdate(tc.found, tc.skipped); got != tc.want {
				t.Fatalf("shouldSurfaceUpdate(%q, %q) = %v, want %v", tc.found, tc.skipped, got, tc.want)
			}
		})
	}
}

// TestSkipUpdateVersion proves the user's "Skip This Version" choice is
// persisted to the settings table (so it survives restarts) and round-trips
// through the same key the background check reads.
func TestSkipUpdateVersion(t *testing.T) {
	a := newUpdaterTestApp(t)

	// Nothing skipped initially.
	if v, err := a.GetSetting(settingSkippedUpdateVersion); err != nil || v != "" {
		t.Fatalf("initial skipped = %q, err %v; want empty", v, err)
	}

	if err := a.SkipUpdateVersion("26.6.6-rc.51"); err != nil {
		t.Fatalf("SkipUpdateVersion: %v", err)
	}

	v, err := a.GetSetting(settingSkippedUpdateVersion)
	if err != nil {
		t.Fatalf("GetSetting: %v", err)
	}
	if v != "26.6.6-rc.51" {
		t.Fatalf("persisted skipped = %q, want 26.6.6-rc.51", v)
	}

	// The persisted value is exactly what the background gate compares against,
	// so the just-skipped version must now be suppressed.
	if shouldSurfaceUpdate("26.6.6-rc.51", v) {
		t.Fatal("skipped version should be suppressed after SkipUpdateVersion")
	}
	// A newer version must still surface.
	if !shouldSurfaceUpdate("26.6.7", v) {
		t.Fatal("a newer version must still surface after skipping an older one")
	}
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
