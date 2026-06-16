package main

import (
	"embed"
	"runtime"

	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	// Create the IRC application backend (storage, event bus, plugins, …).
	ircApp, err := NewApp()
	if err != nil {
		println("Error initializing app:", err.Error())
		return
	}

	// Create the Wails application. In v3 the backend is registered as a
	// service: its ServiceStartup/ServiceShutdown hooks drive connect/cleanup,
	// and its exported methods are bound for the frontend. v3 installs its own
	// signal handler, so the manual SIGTERM/SIGINT plumbing from v2 is gone.
	app := application.New(application.Options{
		Name:        "Cascade Chat",
		Description: "Modern multi-platform IRC client",
		Services: []application.Service{
			application.NewService(ircApp),
		},
		Assets: application.AssetOptions{
			Handler: application.BundledAssetFileServer(assets),
		},
	})

	// Build and attach the application menu.
	app.Menu.SetApplicationMenu(buildMenu(app, ircApp))

	// Create the main window. v2's options.App size/colour fields move onto the
	// per-window options struct in v3.
	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "Cascade Chat",
		Width:            1280,
		Height:           800,
		MinWidth:         940,
		MinHeight:        600,
		BackgroundColour: application.NewRGB(27, 38, 54),
	})

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

	// Help menu with an About entry that opens the About settings pane.
	helpMenu := menu.AddSubmenu("Help")
	helpMenu.Add("About Cascade").OnClick(func(*application.Context) {
		ircApp.OpenSettingsAbout()
	})

	return menu
}
