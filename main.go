package main

import (
	"embed"
	"net/http"
	_ "net/http/pprof" // registers /debug/pprof handlers on http.DefaultServeMux
	"os"
	"runtime"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/updater"
	"github.com/wailsapp/wails/v3/pkg/updater/providers/github"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	// Optional pprof server for diagnosing hangs/deadlocks (e.g. a wedged send or
	// a stuck connection read loop). Off unless CASCADE_PPROF is set, and binds
	// loopback only so it is never network-exposed. Set it to "1" for the default
	// 127.0.0.1:6060, or to an explicit host:port. Grab a full goroutine dump with
	//   curl 'http://127.0.0.1:6060/debug/pprof/goroutine?debug=2'
	if v := os.Getenv("CASCADE_PPROF"); v != "" {
		addr := "127.0.0.1:6060"
		if strings.Contains(v, ":") {
			addr = v
		}
		go func() { _ = http.ListenAndServe(addr, nil) }()
	}

	// Create the IRC application backend (storage, event bus, plugins, …).
	ircApp, err := NewApp()
	if err != nil {
		println("Error initializing app:", err.Error())
		return
	}

	// Native notifications are gated behind a build tag (notify.go /
	// notify_server.go): the headless `server` build returns nil and skips both
	// registering the service and wiring it. macOS notifications require a valid
	// app-bundle identifier, which the bare server binary lacks — registering the
	// service fails its startup ("notifications require a valid bundle
	// identifier") and AttachNotifications would abort in CGO.
	ns := newNotificationService()

	// Create the Wails application. In v3 the backend is registered as a
	// service: its ServiceStartup/ServiceShutdown hooks drive connect/cleanup,
	// and its exported methods are bound for the frontend. v3 installs its own
	// signal handler, so the manual SIGTERM/SIGINT plumbing from v2 is gone.
	services := []application.Service{application.NewService(ircApp)}
	if ns != nil {
		services = append(services, application.NewService(ns))
	}
	app := application.New(application.Options{
		Name:        "Cascade Chat",
		Description: "Modern multi-platform IRC client",
		Services:    services,
		Assets: application.AssetOptions{
			Handler: application.BundledAssetFileServer(assets),
		},
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: "com.mattouille.cascade",
			OnSecondInstanceLaunch: func(data application.SecondInstanceData) {
				for _, arg := range data.Args {
					if isDeepLinkArg(arg) {
						ircApp.handleDeepLink(arg)
					}
				}
			},
		},
	})

	// Opt out of macOS automatic window tabbing so the framework's updater
	// window opens as its own standalone window instead of being merged into
	// the main window's tab bar (which resizes the unified window). No-op off
	// macOS. See window_tabbing_darwin.go.
	configurePlatformWindowBehavior(app)

	// Wire up in-app self-updates. The updater is only configured for real
	// release builds: dev and CI-merge builds carry a non-release version
	// string (see isReleaseVersion) and must never replace themselves with a
	// published release. When configured, app.Updater drives the framework's
	// download → verify → atomic-swap → relaunch flow against GitHub Releases,
	// authenticating each download against the release's SHA256SUMS sidecar.
	// The window is themed to the user's persisted accent. The periodic check
	// is started later from ServiceStartup (see app_updater.go) rather than via
	// Config.CheckInterval, so background polling never pops the window unless
	// an update is actually found.
	if isReleaseVersion(version) {
		// The channel-routing provider reads the persisted "updateChannel"
		// setting live on every check (see newGitHubChannelProvider), so the
		// Stable/Prerelease choice takes effect on the next check without an app
		// restart — even though Updater.Init below is single-shot.
		gh, err := newGitHubChannelProvider(ircApp.updateChannelPrerelease)
		if err != nil {
			println("Error creating updater provider:", err.Error())
		} else if err := app.Updater.Init(updater.Config{
			CurrentVersion: version,
			Providers:      []updater.Provider{gh},
			Window:         &updater.BuiltinWindow{CSS: ircApp.updaterAccentCSS()},
		}); err != nil {
			println("Error initializing updater:", err.Error())
		}
	}

	// Build and attach the application menu.
	app.Menu.SetApplicationMenu(buildMenu(app, ircApp))

	// Create the main window. v2's options.App size/colour fields move onto the
	// per-window options struct in v3.
	mainWindow := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "Cascade Chat",
		Width:            1280,
		Height:           800,
		MinWidth:         940,
		MinHeight:        600,
		BackgroundColour: application.NewRGB(27, 38, 54),
	})

	// Wire native notifications now that the service and window exist. Routed
	// through a build-tagged seam (notify.go / notify_server.go) because the
	// headless `server` build must skip it: the bare binary has no macOS app
	// bundle, so registering notification categories aborts the process at
	// startup (a CGO exception the Go error guards can't catch).
	attachNotifications(ircApp, ns, mainWindow)

	if err := app.Run(); err != nil {
		println("Error:", err.Error())
	}
}

// buildMenu constructs the application menu. The platform-specific role menus
// (App/Edit/Window) are macOS-only — on Linux/Windows they are populated by the
// OS and carry no custom items, so we only add the standard roles on darwin.
// Unlike v2, v3 owns its cross-platform menu builder, so the GTK nil-deref
// workaround the v2 code needed is no longer required.
func buildMenu(app *application.App, ircApp *App) *application.Menu {
	menu := app.NewMenu()

	if runtime.GOOS == "darwin" {
		menu.AddRole(application.AppMenu)
	}

	// File menu with Settings and shortcuts (Cmd+, is standard for Settings).
	fileMenu := menu.AddSubmenu("File")
	fileMenu.Add("Settings...").SetAccelerator("CmdOrCtrl+,").OnClick(func(*application.Context) {
		ircApp.OpenSettings()
	})
	fileMenu.AddSeparator()
	fileMenu.Add("Networks...").SetAccelerator("CmdOrCtrl+N").OnClick(func(*application.Context) {
		ircApp.OpenSettingsNetworks()
	})
	fileMenu.Add("Plugins...").SetAccelerator("CmdOrCtrl+P").OnClick(func(*application.Context) {
		ircApp.OpenSettingsPlugins()
	})
	fileMenu.Add("Display...").SetAccelerator("CmdOrCtrl+D").OnClick(func(*application.Context) {
		ircApp.OpenSettingsDisplay()
	})

	if runtime.GOOS == "darwin" {
		menu.AddRole(application.EditMenu)
		menu.AddRole(application.WindowMenu)
	}

	// Help menu with a manual update check and an About entry that opens the
	// About settings pane. CheckForUpdates is a no-op (with a frontend toast)
	// on dev builds where the updater was never configured.
	helpMenu := menu.AddSubmenu("Help")
	helpMenu.Add("Check for Updates…").OnClick(func(*application.Context) {
		ircApp.CheckForUpdates()
	})
	helpMenu.Add("About Cascade").OnClick(func(*application.Context) {
		ircApp.OpenSettingsAbout()
	})

	return menu
}

// matchUpdateAsset selects the release asset the in-app updater should download
// for the running client's OS+arch. The stock github.DefaultAssetMatcher keys on
// GOOS+GOARCH substrings, but Cascade's asset names and update-container formats
// differ per platform, so we match explicitly and let siblings (the .dmg, the
// NSIS installer, the .deb/.rpm/.AppImage, SHA256SUMS) fall through:
//   - darwin: the single universal .zip (carries the .app; no arch in its name)
//   - windows: the per-arch .zip wrapping the bare cascade.exe
//   - linux: the per-arch .tar.gz wrapping the bare cascade binary
//
// Only the bare-binary archive is updater-eligible per platform — the framework
// swaps os.Executable() in place after extracting a .zip/.tar.gz, so the .dmg and
// the Linux installers (deb/rpm/AppImage) are download-only and deliberately not
// matched. Matching is case-insensitive and accepts common arch aliases so a
// mismatched arch never resolves. Returns the asset index, or -1 for no match
// (the github provider turns that into a clean "no asset" error, not a panic).
func matchUpdateAsset(req updater.CheckRequest, assets []github.ReleaseAsset) int {
	for i, a := range assets {
		name := strings.ToLower(a.Name)
		switch req.Platform {
		case "darwin":
			if strings.Contains(name, "universal") && strings.HasSuffix(name, ".zip") {
				return i
			}
		case "windows":
			if strings.Contains(name, "windows") && strings.HasSuffix(name, ".zip") && archMatches(name, req.Arch) {
				return i
			}
		case "linux":
			if strings.Contains(name, "linux") && strings.HasSuffix(name, ".tar.gz") && archMatches(name, req.Arch) {
				return i
			}
		}
	}
	return -1
}

// archMatches reports whether a (lowercased) asset name targets the given GOARCH.
// It accepts the literal GOARCH plus the common aliases shippers use — x86_64/x64
// for amd64 and aarch64 for arm64 — so a renamed asset still resolves while an
// amd64 client never matches an arm64 asset (and vice-versa).
func archMatches(lowerName, arch string) bool {
	if strings.Contains(lowerName, arch) {
		return true
	}
	switch arch {
	case "amd64":
		return strings.Contains(lowerName, "x86_64") || strings.Contains(lowerName, "x64")
	case "arm64":
		return strings.Contains(lowerName, "aarch64")
	}
	return false
}

// isReleaseVersion reports whether v looks like a published release version
// rather than a dev build. version.go defaults to "dev"; CI merge-to-main
// builds stamp "dev-<sha>". Only real releases (e.g. "1.2.0") enable the
// updater, so a dev build never tries to swap itself toward a published
// artifact.
func isReleaseVersion(v string) bool {
	return v != "" && v != "dev" && !strings.HasPrefix(v, "dev-")
}
