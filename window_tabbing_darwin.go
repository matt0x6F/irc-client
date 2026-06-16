package main

/*
#cgo LDFLAGS: -framework Cocoa
#include "window_tabbing_darwin.h"
*/
import "C"

import (
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// configurePlatformWindowBehavior opts the whole app out of macOS automatic
// window tabbing. Without it, AppKit merges any second window — notably the
// framework's updater window — into the main window's tab bar whenever "Prefer
// tabs" is enabled or the main window is fullscreen, then resizes the unified
// window to fit the new tab. Cascade is a single-window app with no
// document-tab concept, so disabling automatic tabbing app-wide is the correct
// fix; it mirrors what Electron/Chromium do by default.
//
// The opt-out is applied from the ApplicationDidFinishLaunching hook so it runs
// once NSApplication is up. Note Wails delivers this (and every other
// application-event listener) on a background goroutine, not the main thread —
// so disableAutomaticWindowTabbing itself dispatches the actual AppKit setter
// to the main queue (see window_tabbing_darwin.m); calling it inline here is a
// silent no-op. It only has to take effect before the updater window is ever
// shown, which is always long after launch.
func configurePlatformWindowBehavior(app *application.App) {
	app.Event.OnApplicationEvent(events.Mac.ApplicationDidFinishLaunching, func(*application.ApplicationEvent) {
		C.disableAutomaticWindowTabbing()
	})
}
