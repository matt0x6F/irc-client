package main

import (
	"context"
	"io"
	"time"

	"github.com/matt0x6f/irc-client/internal/logger"
	"github.com/wailsapp/wails/v3/pkg/updater"
	"github.com/wailsapp/wails/v3/pkg/updater/providers/github"
)

// updateCheckInterval is how often the background loop polls for updates.
const updateCheckInterval = 6 * time.Hour

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
	// Run off the caller's goroutine; the window and event bus report progress.
	go func() {
		if err := a.app.Updater.CheckAndInstall(context.Background()); err != nil {
			logger.Log.Warn().Err(err).Msg("update check failed")
		}
	}()
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
		AssetMatcher:  matchDarwinUniversalZip,
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

// startPeriodicUpdateCheck runs a background loop that polls the provider chain
// on updateCheckInterval and surfaces the updater window only when a newer
// release is found. It deliberately uses Updater.Check (which opens no window)
// rather than Config.CheckInterval / CheckAndInstall, so a routine up-to-date
// poll never pops a window at the user; only a found update escalates to
// CheckAndInstall. Updater.Check already treats a skipped version as
// up-to-date, so "Skip This Version" is honoured for free.
//
// It is a no-op on dev builds where the updater was never configured. The loop
// is tracked by startupWg and cancelled when startupCtx is closed in
// ServiceShutdown.
func (a *App) startPeriodicUpdateCheck() {
	if a.app == nil || a.app.Updater.State() == updater.StateUnconfigured {
		return
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
				rel, err := a.app.Updater.Check(a.startupCtx)
				if err != nil {
					logger.Log.Debug().Err(err).Msg("periodic update check failed")
					continue
				}
				if rel == nil {
					continue // up to date (or the available version was skipped)
				}
				logger.Log.Info().Str("version", rel.Version).Msg("update available; surfacing updater window")
				if err := a.app.Updater.CheckAndInstall(a.startupCtx); err != nil {
					logger.Log.Warn().Err(err).Msg("update install flow failed")
				}
			}
		}
	}()
}
