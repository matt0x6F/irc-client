package main

import (
	"context"
	"embed"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	// Create an instance of the app structure
	app, err := NewApp()
	if err != nil {
		println("Error initializing app:", err.Error())
		return
	}

	// Set up signal handling to ensure shutdown is called
	// This is a backup in case OnShutdown doesn't get called
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-sigChan
		fmt.Printf("Received signal: %v, initiating shutdown\n", sig)
		// Create a context for shutdown
		ctx := context.Background()
		app.shutdown(ctx)
	}()

	// Create application menu
	appMenu := menu.NewMenu()

	// On macOS, add the standard app menu
	if runtime.GOOS == "darwin" {
		appMenu.Append(menu.AppMenu())
	}

	// Add File menu with Settings and shortcuts
	fileMenu := menu.NewMenu()
	// Parse the comma shortcut (Cmd+, is standard for Settings/Preferences)
	settingsAccel, err := keys.Parse("CmdOrCtrl+,")
	if err != nil {
		// Fallback to CmdOrCtrl+S if comma doesn't work
		settingsAccel = keys.CmdOrCtrl("S")
	}
	fileMenu.AddText("Settings...", settingsAccel, func(_ *menu.CallbackData) {
		// Emit event to frontend to open settings
		app.OpenSettings()
	})
	fileMenu.AddSeparator()
	fileMenu.AddText("Networks...", keys.CmdOrCtrl("N"), func(_ *menu.CallbackData) {
		app.OpenSettingsNetworks()
	})
	fileMenu.AddText("Plugins...", keys.CmdOrCtrl("P"), func(_ *menu.CallbackData) {
		app.OpenSettingsPlugins()
	})
	fileMenu.AddText("Display...", keys.CmdOrCtrl("D"), func(_ *menu.CallbackData) {
		app.OpenSettingsDisplay()
	})
	appMenu.Append(menu.SubMenu("File", fileMenu))

	// Add Edit menu (on macOS, add it after File for proper ordering)
	if runtime.GOOS == "darwin" {
		appMenu.Append(menu.EditMenu())
	}

	// Add standard menus
	if runtime.GOOS != "darwin" {
		appMenu.Append(menu.EditMenu())
	}
	appMenu.Append(menu.WindowMenu())

	// Create application with options
	err = wails.Run(&options.App{
		Title:  "Cascade Chat",
		Width:  1024,
		Height: 768,
		Menu:   appMenu,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 27, G: 38, B: 54, A: 1},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}
