package main

import (
	"crypto/rand"
	"encoding/hex"
	"strconv"
	"strings"

	"github.com/matt0x6f/irc-client/internal/logger"
	"github.com/matt0x6f/irc-client/internal/notification"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"
)

// Notification category and action identifiers, shared with the response router.
const (
	notifyCategoryMessage    = "message"
	notifyCategoryConnection = "connection"

	notifyActionReply     = "REPLY"
	notifyActionMarkRead  = "MARK_READ"
	notifyActionReconnect = "RECONNECT"
)

// wailsDelivery adapts the Wails notifications service to notification.Delivery,
// translating our Wails-free structs into the framework's option types.
type wailsDelivery struct {
	svc *notifications.NotificationService
}

func newWailsDelivery(svc *notifications.NotificationService) *wailsDelivery {
	return &wailsDelivery{svc: svc}
}

func (d *wailsDelivery) toOptions(n notification.Notification) notifications.NotificationOptions {
	return notifications.NotificationOptions{
		ID:         n.ID,
		Title:      n.Title,
		Subtitle:   n.Subtitle,
		Body:       n.Body,
		CategoryID: n.CategoryID,
		Data:       n.Data,
	}
}

func (d *wailsDelivery) toCategory(c notification.Category) notifications.NotificationCategory {
	actions := make([]notifications.NotificationAction, len(c.Actions))
	for i, a := range c.Actions {
		actions[i] = notifications.NotificationAction{ID: a.ID, Title: a.Title, Destructive: a.Destructive}
	}
	return notifications.NotificationCategory{
		ID:               c.ID,
		Actions:          actions,
		HasReplyField:    c.HasReplyField,
		ReplyPlaceholder: c.ReplyPlaceholder,
		ReplyButtonTitle: c.ReplyButtonTitle,
	}
}

func (d *wailsDelivery) Send(n notification.Notification) error {
	return d.svc.SendNotification(d.toOptions(n))
}
func (d *wailsDelivery) SendWithActions(n notification.Notification) error {
	return d.svc.SendNotificationWithActions(d.toOptions(n))
}
func (d *wailsDelivery) RegisterCategory(c notification.Category) error {
	return d.svc.RegisterNotificationCategory(d.toCategory(c))
}
func (d *wailsDelivery) RequestAuthorization() (bool, error) {
	return d.svc.RequestNotificationAuthorization()
}
func (d *wailsDelivery) CheckAuthorization() (bool, error) {
	return d.svc.CheckNotificationAuthorization()
}

// AttachNotifications wires the Wails notification service into the app: it
// installs the delivery backend, registers the categories, hooks the response
// callback, captures the main window for focus, and seeds the prefs snapshot.
func (a *App) AttachNotifications(svc *notifications.NotificationService, win *application.WebviewWindow) {
	a.notifyWindow = win
	d := newWailsDelivery(svc)
	a.notifier.SetDelivery(d)

	if err := d.RegisterCategory(messageCategory()); err != nil {
		logger.Log.Debug().Err(err).Msg("register message notification category")
	}
	if err := d.RegisterCategory(connectionCategory()); err != nil {
		logger.Log.Debug().Err(err).Msg("register connection notification category")
	}

	svc.OnNotificationResponse(a.handleNotificationResponse)
	a.refreshNotificationPrefs()
}

func messageCategory() notification.Category {
	return notification.Category{
		ID: notifyCategoryMessage,
		Actions: []notification.Action{
			{ID: notifyActionReply, Title: "Reply"},
			{ID: notifyActionMarkRead, Title: "Mark as read"},
		},
		HasReplyField:    true,
		ReplyPlaceholder: "Message…",
		ReplyButtonTitle: "Send",
	}
}

func connectionCategory() notification.Category {
	return notification.Category{
		ID:      notifyCategoryConnection,
		Actions: []notification.Action{{ID: notifyActionReconnect, Title: "Reconnect"}},
	}
}

// RequestNotificationPermission asks the OS for notification permission. Bound to
// the frontend; called when the user enables notifications in settings.
func (a *App) RequestNotificationPermission() (bool, error) {
	return a.notifier.RequestAuthorization()
}

// FocusMainWindow raises the main window. Bound to the frontend and called from
// the notification:navigate handler so window focus runs on the framework's
// webview-request path rather than the off-main notification callback.
func (a *App) FocusMainWindow() {
	if a.notifyWindow != nil {
		a.notifyWindow.Focus()
	}
}

// refreshNotificationPrefs reloads the notifications.* settings into the notifier.
func (a *App) refreshNotificationPrefs() {
	a.notifier.SetPrefs(notification.Prefs{
		Enabled:           a.boolSetting("notifications.enabled", false),
		PrivateMessages:   a.boolSetting("notifications.privateMessages", true),
		Mentions:          a.boolSetting("notifications.mentions", true),
		ConnectionLost:    a.boolSetting("notifications.connectionLost", true),
		OnlyWhenUnfocused: a.boolSetting("notifications.onlyWhenUnfocused", true),
	})
}

func (a *App) boolSetting(key string, def bool) bool {
	v, err := a.storage.GetSetting(key)
	if err != nil || v == "" {
		return def
	}
	return v == "true"
}

// reconnectNetworkByID rebuilds the saved network config and reconnects it,
// flagging the client to rejoin every channel it was in.
func (a *App) reconnectNetworkByID(networkID int64) error {
	network, err := a.storage.GetNetwork(networkID)
	if err != nil {
		return err
	}
	cfg, err := a.buildReconnectConfig(networkID, network)
	if err != nil {
		return err
	}
	return a.connectNetwork(cfg, true)
}

// notifyResponseDeps are the seams routeNotificationResponse needs, so the
// routing logic is unit-testable without a live app.
type notifyResponseDeps struct {
	emit        func(name string, data ...any)
	sendMessage func(networkID int64, target, message string) error
	reconnect   func(networkID int64) error
}

// handleNotificationResponse is the live callback Wails invokes off the main
// thread. It builds the deps from the app and delegates to the pure router.
func (a *App) handleNotificationResponse(result notifications.NotificationResult) {
	if result.Error != nil {
		logger.Log.Debug().Err(result.Error).Msg("notification response error")
		return
	}
	routeNotificationResponse(result.Response, notifyResponseDeps{
		emit:        a.emit,
		sendMessage: a.SendMessage,
		reconnect:   a.reconnectNetworkByID,
	})
}

// routeNotificationResponse dispatches a notification response. UI-state actions
// (navigate, mark-read) emit events to the frontend; server-state actions (reply,
// reconnect) run in Go.
func routeNotificationResponse(resp notifications.NotificationResponse, deps notifyResponseDeps) {
	networkID, _ := parseNetworkID(resp.UserInfo["networkId"])
	target, _ := resp.UserInfo["target"].(string)

	switch resp.ActionIdentifier {
	case notifications.DefaultActionIdentifier:
		deps.emit("notification:navigate", map[string]any{"networkId": networkID, "target": target})
	case notifyActionMarkRead:
		deps.emit("notification:markRead", map[string]any{"networkId": networkID, "target": target})
	case notifyActionReply:
		if resp.UserText == "" {
			return
		}
		sendTarget := target
		if strings.HasPrefix(target, "pm:") {
			sendTarget = strings.TrimPrefix(target, "pm:")
		}
		if err := deps.sendMessage(networkID, sendTarget, resp.UserText); err != nil {
			logger.Log.Debug().Err(err).Msg("notification reply send failed")
		}
	case notifyActionReconnect:
		if err := deps.reconnect(networkID); err != nil {
			logger.Log.Debug().Err(err).Msg("notification reconnect failed")
		}
	}
}

// parseNetworkID extracts an int64 network id from a UserInfo value that may have
// crossed the JSON boundary (string is the canonical form; float64/int64 handled
// defensively).
func parseNetworkID(v any) (int64, bool) {
	switch t := v.(type) {
	case string:
		n, err := strconv.ParseInt(t, 10, 64)
		return n, err == nil
	case float64:
		return int64(t), true
	case int64:
		return t, true
	}
	return 0, false
}

// newNotificationID returns a random hex id for a notification.
func newNotificationID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
