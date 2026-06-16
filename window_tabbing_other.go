//go:build !darwin

package main

import "github.com/wailsapp/wails/v3/pkg/application"

// configurePlatformWindowBehavior is a no-op off macOS: automatic window
// tabbing is a macOS-only behaviour, so there is nothing to opt out of on
// Windows or Linux. The darwin build (see window_tabbing_darwin.go) carries the
// real implementation.
func configurePlatformWindowBehavior(app *application.App) {}
