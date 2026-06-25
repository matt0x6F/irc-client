//go:build server

package main

import (
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"
)

// newNotificationService returns nil in the headless `server` build so the
// caller skips registering the notification service entirely. macOS notification
// service startup requires a valid app-bundle identifier, which the bare server
// binary lacks — registering it fails with "notifications require a valid bundle
// identifier" and the binary exits before serving.
func newNotificationService() *notifications.NotificationService {
	return nil
}

// attachNotifications is a no-op in the headless `server` build (used by the e2e
// / screenshot harness). Beyond the service-startup failure above, the bare
// binary also has no app bundle, so registering UNUserNotificationCenter
// categories — what App.AttachNotifications does via RegisterNotificationCategory
// — aborts the process with a CGO (Objective-C) exception that the Go error
// guards can't catch. The server harness drives the app as a web page and never
// uses native notifications, so skipping the wiring is safe.
func attachNotifications(_ *App, _ *notifications.NotificationService, _ *application.WebviewWindow) {
}
