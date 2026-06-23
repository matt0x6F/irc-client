package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/matt0x6f/irc-client/internal/logger"
	"github.com/wailsapp/wails/v3/pkg/updater"
	"github.com/wailsapp/wails/v3/pkg/updater/providers/github"
)

// updateCheckInterval is how often the background loop polls for updates.
const updateCheckInterval = 6 * time.Hour

// settingSkippedUpdateVersion is the settings-table key holding the release the
// user dismissed via the in-app "Skip This Version" button. The background
// check reads it to suppress re-surfacing that exact version; it persists
// across restarts (the framework updater's own skip state is in-memory only).
// A manual "Check for Updates…" deliberately ignores it — an explicit check is
// the user asking to see what's available regardless of a past skip.
const settingSkippedUpdateVersion = "updater.skippedVersion"

// shouldSurfaceUpdate reports whether a release found by a background check
// should be surfaced to the user. A non-empty version surfaces unless it
// exactly matches the version the user skipped — so a newer (or any different)
// release still prompts, but the skipped one stays quiet. The match is exact:
// "skip" suppresses one specific version, never a range.
func shouldSurfaceUpdate(foundVersion, skippedVersion string) bool {
	return foundVersion != "" && foundVersion != skippedVersion
}

// releasesURL is where users sent to a manual update (a non-self-updating Linux
// install) can download a newer build by hand. It mirrors the repo configured in
// newGitHubChannelProvider.
const releasesURL = "https://github.com/matt0x6f/irc-client/releases/latest"

// osExecutable is a seam over os.Executable so tests can simulate the install
// location when exercising linuxSelfUpdateEligible.
var osExecutable = os.Executable

// CheckForUpdates is the Wails-bound manual update trigger, called from the
// "Check for Updates…" menu item and the About-pane button. On a dev build the
// updater was never configured (see isReleaseVersion in main.go); rather than
// silently doing nothing, we emit updater:unavailable so the frontend can toast
// a friendly explanation. Otherwise CheckAndInstall opens the framework's themed
// updater window, checks GitHub, and downloads/verifies/stages a newer release
// if one exists. A manual check intentionally always shows the window (including
// the reassuring "You're Up to Date" state).
func (a *App) CheckForUpdates() {
	if a.app == nil {
		return
	}
	if a.app.Updater.State() == updater.StateUnconfigured {
		a.emit("updater:unavailable")
		return
	}
	// On Linux, only a bare-binary install in a writable dir can self-update;
	// AppImage and system (deb/rpm) installs would fail the in-place swap (and
	// the framework has no abort hook once CheckAndInstall starts), so surface a
	// "download it" prompt instead of marching toward a doomed swap.
	if runtime.GOOS == "linux" {
		if ok, reason := linuxSelfUpdateEligible(); !ok {
			logger.Log.Info().Str("reason", reason).Msg("self-update unavailable on this Linux install; prompting manual update")
			a.emit("updater:manual-update", map[string]string{"reason": reason, "url": releasesURL})
			return
		}
	}
	// Run off the caller's goroutine; the window and event bus report progress.
	go func() {
		if err := a.app.Updater.CheckAndInstall(context.Background()); err != nil {
			logger.Log.Warn().Err(err).Msg("update check failed")
		}
	}()
}

// SkipUpdateVersion is the Wails-bound handler for the in-app update prompt's
// "Skip This Version" button. It persists the version to the settings table so
// the background check stops re-surfacing it — including across restarts, which
// the framework updater's own in-memory skip does not survive. A newer release
// still prompts (see shouldSurfaceUpdate), and a manual "Check for Updates…"
// still shows the skipped version, since that is an explicit user request.
func (a *App) SkipUpdateVersion(version string) error {
	if version == "" {
		return nil
	}
	logger.Log.Info().Str("version", version).Msg("user skipped update version")
	return a.SetSetting(settingSkippedUpdateVersion, version)
}

// updaterAccentCSS returns a CSS snippet that themes the framework's built-in
// updater window to the user's persisted accent colour. The accent ids and
// swatches mirror frontend/src/stores/theme.ts; an unknown or missing value
// falls back to the default blue. Safe to call before the app is running
// because storage is opened in NewApp.
//
// It also patches a bug in the framework's built-in window stylesheet: the base
// ".u__btn:hover" rule sets background to var(--surface-2), and the primary
// button's own :hover rule only sets `filter` — so on hover the "Restart &
// Apply" / "Install Update" buttons drop to a near-white surface while keeping
// white --accent-fg text, making the label vanish. We re-assert the accent
// background on primary hover; because this snippet is appended after the
// template's <style>, it wins on the source-order tiebreak (equal specificity).
func (a *App) updaterAccentCSS() string {
	hex := "#2563eb" // blue (the frontend default accent)
	if a.storage != nil {
		if v, err := a.GetSetting("theme.accent"); err == nil {
			switch v {
			case "purple":
				hex = "#7c3aed"
			case "emerald":
				hex = "#059669"
			case "rose":
				hex = "#e11d48"
			case "amber":
				hex = "#d97706"
			}
		}
	}
	return ":root { --accent: " + hex + "; --accent-fg: #ffffff; }\n" +
		".u__btn--primary:hover:not(:disabled) { background: var(--accent); }"
}

// updateChannelPrerelease reports whether the user opted into the prerelease
// update channel via the persisted "updateChannel" setting. Any unset or
// unknown value means the default stable channel (false), so the updater only
// ever tracks prereleases when the user has explicitly opted in. This is passed
// (as a func) to newGitHubChannelProvider and read live on every update check,
// so flipping the channel in Settings takes effect on the next check without an
// app restart. Safe to call before the app is running because storage is opened
// in NewApp.
func (a *App) updateChannelPrerelease() bool {
	if a.storage == nil {
		return false
	}
	v, err := a.GetSetting("updateChannel")
	return err == nil && v == "prerelease"
}

// channelRoutingProvider wraps a stable and a prerelease github.Provider and
// dispatches each call to whichever the live "updateChannel" setting selects.
//
// The Wails Updater.Init is single-shot, so we cannot rebuild the provider when
// the user flips the channel mid-session. Instead we register ONE provider that
// reads the setting (via usePrerelease) on every Check/Download, so a channel
// change takes effect on the very next check with no app restart. This fixes the
// updater reporting "You're Up to Date" against the stable /releases/latest
// while a prerelease was selected: a stable channel sees only v26.6.4, which is
// older than a running 26.6.5-rc.N, so nothing is ever offered.
type channelRoutingProvider struct {
	stable        updater.Provider
	prerelease    updater.Provider
	usePrerelease func() bool
}

// Name is fixed (not derived from the active channel) so the Updater stamps a
// stable provider id on each Release and routes the follow-up Download back to
// this same wrapper. Both underlying providers are github-backed.
func (p *channelRoutingProvider) Name() string { return "github" }

// active resolves the underlying provider for the current channel, read live.
func (p *channelRoutingProvider) active() updater.Provider {
	if p.usePrerelease() {
		return p.prerelease
	}
	return p.stable
}

func (p *channelRoutingProvider) Check(ctx context.Context, req updater.CheckRequest) (*updater.Release, error) {
	return p.active().Check(ctx, req)
}

func (p *channelRoutingProvider) Download(ctx context.Context, r *updater.Release, dst io.Writer, onProgress func(written, total int64)) error {
	return p.active().Download(ctx, r, dst, onProgress)
}

// newGitHubChannelProvider builds the channel-routing github provider used by
// the updater. It instantiates two providers up front — one tracking stable
// releases (/releases/latest), one tracking prereleases (/releases) — and routes
// between them on each call via usePrerelease, so a channel switch needs no
// restart. usePrerelease is read live (typically App.updateChannelPrerelease).
func newGitHubChannelProvider(usePrerelease func() bool) (updater.Provider, error) {
	base := github.Config{
		Repository:    "matt0x6f/irc-client",
		ChecksumAsset: "SHA256SUMS",
		AssetMatcher:  matchUpdateAsset,
	}
	stableCfg := base
	stableCfg.Prerelease = false
	stable, err := github.New(stableCfg)
	if err != nil {
		return nil, err
	}
	preCfg := base
	preCfg.Prerelease = true
	pre, err := github.New(preCfg)
	if err != nil {
		return nil, err
	}
	return &channelRoutingProvider{
		stable:        stable,
		prerelease:    pre,
		usePrerelease: usePrerelease,
	}, nil
}

// linuxSelfUpdateEligible reports whether the running Linux install can swap
// itself in place, and if not, a short reason for the manual-update message.
//
// The Wails updater replaces the path os.Executable() returns (verified against
// the vendored updater: bundleTarget is the identity off darwin, and helper_unix
// does unlink+rename there). That works only for a bare binary in a user-writable
// directory. Two installs break it and must degrade to a manual download instead:
//   - AppImage: os.Executable() resolves to the squashfs mount (/tmp/.mount_*),
//     not the .AppImage file (whose real path is in $APPIMAGE), so a swap would
//     corrupt the mount and never touch the actual artifact. Detected via the
//     $APPIMAGE env var, with a /tmp/.mount_ path check as a belt-and-suspenders.
//   - System packages (.deb/.rpm under root-owned /usr/bin): a user-level swap
//     fails with EACCES, and because the helper is detached and the app has
//     already quit, the failure is silent. Detected by probing the install dir
//     for writability.
//
// Returns (true, "") when eligible. This is only meaningful on Linux; callers
// gate on runtime.GOOS == "linux" before consulting it.
func linuxSelfUpdateEligible() (bool, string) {
	if os.Getenv("APPIMAGE") != "" {
		return false, "appimage"
	}
	exe, err := osExecutable()
	if err != nil {
		return false, "unknown"
	}
	if resolved, rerr := filepath.EvalSymlinks(exe); rerr == nil {
		exe = resolved
	}
	if strings.HasPrefix(exe, "/tmp/.mount_") {
		return false, "appimage"
	}
	if !dirWritable(filepath.Dir(exe)) {
		return false, "system"
	}
	return true, ""
}

// dirWritable reports whether the current user can create files in dir, by
// attempting to create and remove a probe file. This is the same directory the
// updater's in-place rename would target, so it accurately predicts whether the
// swap can succeed — more reliable than path heuristics like matching /usr/bin.
func dirWritable(dir string) bool {
	f, err := os.CreateTemp(dir, ".cascade-wtest-*")
	if err != nil {
		return false
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return true
}

// startPeriodicUpdateCheck runs a background loop that polls the provider chain
// on updateCheckInterval and, when a newer release is found, asks the frontend
// to show the in-app update prompt. It deliberately uses Updater.Check (which
// opens no window and downloads nothing) rather than CheckAndInstall: a routine
// poll never pops a window or pre-downloads a release at the user. Only the
// prompt's "Install" button escalates to the download/verify/restart flow.
//
// This is what makes "Skip This Version" actually stick. The framework updater's
// own skip state is in-memory only (lost on restart) and unreachable once a
// release has auto-downloaded to the "Update Ready" state — its only dismiss
// action there is "Close", which records nothing. We instead persist the skip
// in the settings table and gate the surfacing here (see surfaceUpdateIfAvailable).
//
// It is a no-op on dev builds where the updater was never configured. The loop
// is tracked by startupWg and cancelled when startupCtx is closed in
// ServiceShutdown.
func (a *App) startPeriodicUpdateCheck() {
	if a.app == nil || a.app.Updater.State() == updater.StateUnconfigured {
		return
	}
	// A download-only Linux install (AppImage / deb / rpm) must not silently poll
	// and then fail a swap — skip the loop entirely. The user still gets the
	// manual-update prompt via the "Check for Updates…" menu item.
	if runtime.GOOS == "linux" {
		if ok, _ := linuxSelfUpdateEligible(); !ok {
			return
		}
	}
	a.startupWg.Add(1)
	go func() {
		defer a.startupWg.Done()
		ticker := time.NewTicker(updateCheckInterval)
		defer ticker.Stop()
		for {
			select {
			case <-a.startupCtx.Done():
				return
			case <-ticker.C:
				a.surfaceUpdateIfAvailable()
			}
		}
	}()
}

// surfaceUpdateIfAvailable runs one silent background check and, when a release
// is found that the user hasn't skipped, emits updater:update-available so the
// frontend can show the in-app prompt (Install / Remind Me Later / Skip This
// Version). It never opens the framework's updater window or downloads anything
// — that only happens if the user clicks Install (which calls CheckForUpdates).
//
// The skipped-version gate is read from settings on every check, so a skip
// persists across restarts. A newer release still surfaces (shouldSurfaceUpdate
// matches exactly), and "Remind Me Later" persists nothing, so the next poll
// re-prompts as expected.
func (a *App) surfaceUpdateIfAvailable() {
	rel, err := a.app.Updater.Check(a.startupCtx)
	if err != nil {
		logger.Log.Debug().Err(err).Msg("periodic update check failed")
		return
	}
	if rel == nil {
		return // up to date
	}
	skipped, _ := a.GetSetting(settingSkippedUpdateVersion)
	if !shouldSurfaceUpdate(rel.Version, skipped) {
		logger.Log.Debug().Str("version", rel.Version).Msg("update available but suppressed; user skipped this version")
		return
	}
	logger.Log.Info().Str("version", rel.Version).Msg("update available; prompting via in-app dialog")
	a.emit("updater:update-available", map[string]string{
		"currentVersion": a.app.Updater.CurrentVersion(),
		"version":        rel.Version,
		"notes":          rel.Notes,
	})
}
