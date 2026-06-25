//go:build !server

package main

import (
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"
)

// newNotificationService creates the wails notification service for the normal
// (windowed) build, to be registered with the application.
func newNotificationService() *notifications.NotificationService {
	return notifications.New()
}

// attachNotifications wires the native notification service into the app for the
// normal (windowed) build. Split from the call site behind a build tag because
// the headless `server` build must skip it — see notify_server.go.
func attachNotifications(app *App, ns *notifications.NotificationService, win *application.WebviewWindow) {
	app.AttachNotifications(ns, win)
}
