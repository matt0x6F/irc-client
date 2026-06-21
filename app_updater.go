package main

import (
	"context"
	"time"

	"github.com/matt0x6f/irc-client/internal/logger"
	"github.com/wailsapp/wails/v3/pkg/updater"
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
	return ":root { --accent: " + hex + "; --accent-fg: #ffffff; }"
}

// updateChannelPrerelease reports whether the user opted into the prerelease
// update channel via the persisted "updateChannel" setting. Any unset or
// unknown value means the default stable channel (false), so the updater only
// ever tracks prereleases when the user has explicitly opted in. Read once at
// startup from main.go when the github provider is configured; because
// Updater.Init is single-shot (it returns ErrAlreadyConfigured on a second
// call), a channel change only takes effect on the next launch — the Settings
// UI says as much. Safe to call before the app is running because storage is
// opened in NewApp.
func (a *App) updateChannelPrerelease() bool {
	if a.storage == nil {
		return false
	}
	v, err := a.GetSetting("updateChannel")
	return err == nil && v == "prerelease"
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
