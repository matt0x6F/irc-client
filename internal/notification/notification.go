// Package notification decides when to raise a desktop notification and delegates
// the actual delivery to a platform-specific Delivery (wired in by the main
// package). It deliberately imports no Wails packages so the policy stays
// unit-testable.
package notification

import (
	"strings"
	"sync"

	"github.com/matt0x6f/irc-client/internal/logger"
)

// Prefs is a snapshot of the user's notification preferences. The main package
// pushes it in via SetPrefs on startup and whenever a notifications.* setting changes.
type Prefs struct {
	Enabled           bool
	PrivateMessages   bool
	Mentions          bool
	ConnectionLost    bool
	OnlyWhenUnfocused bool
}

// Action is a button on a notification.
type Action struct {
	ID          string
	Title       string
	Destructive bool // macOS-only styling
}

// Category groups actions (and an optional reply field) for reuse across notifications.
type Category struct {
	ID               string
	Actions          []Action
	HasReplyField    bool
	ReplyPlaceholder string
	ReplyButtonTitle string
}

// Notification is a platform-agnostic notification request. Data values must be
// strings — the map round-trips through the OS/JSON boundary on the way back.
type Notification struct {
	ID         string
	Title      string
	Subtitle   string
	Body       string
	CategoryID string
	Data       map[string]any
}

// Delivery is the platform delivery backend, implemented in the main package by
// an adapter over the Wails notifications service.
type Delivery interface {
	Send(Notification) error
	SendWithActions(Notification) error
	RegisterCategory(Category) error
	RequestAuthorization() (bool, error)
	CheckAuthorization() (bool, error)
}

// Notifier decides whether to notify and forwards to the Delivery.
type Notifier struct {
	focused  bool
	prefs    Prefs
	delivery Delivery
	mu       sync.RWMutex
}

// NewNotifier creates a new Notifier.
func NewNotifier() *Notifier {
	return &Notifier{}
}

// SetFocused updates whether the app window is currently focused.
func (n *Notifier) SetFocused(focused bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.focused = focused
}

// IsFocused returns whether the app window is currently focused.
func (n *Notifier) IsFocused() bool {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.focused
}

// SetDelivery installs the platform delivery backend.
func (n *Notifier) SetDelivery(d Delivery) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.delivery = d
}

// SetPrefs replaces the current preferences snapshot.
func (n *Notifier) SetPrefs(p Prefs) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.prefs = p
}

// Prefs returns the current preferences snapshot.
func (n *Notifier) Prefs() Prefs {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.prefs
}

// RequestAuthorization asks the platform for notification permission. A nil
// delivery (e.g. unbundled/dev) reports (false, nil).
func (n *Notifier) RequestAuthorization() (bool, error) {
	n.mu.RLock()
	d := n.delivery
	n.mu.RUnlock()
	if d == nil {
		return false, nil
	}
	return d.RequestAuthorization()
}

// Send delivers an interactive notification. Safe to call from a goroutine; a nil
// delivery or a delivery error is logged at debug and otherwise ignored.
func (n *Notifier) Send(notif Notification) {
	n.mu.RLock()
	d := n.delivery
	n.mu.RUnlock()
	if d == nil {
		return
	}
	if err := d.SendWithActions(notif); err != nil {
		logger.Log.Debug().Err(err).Str("title", notif.Title).Msg("Desktop notification failed")
	}
}

// ShouldNotify is the coarse gate: notifications enabled, focus rule satisfied,
// and at least one relevant category enabled for the event. The caller refines
// the private-message vs mention decision using Prefs().
func (n *Notifier) ShouldNotify(eventType string, _ map[string]interface{}) bool {
	n.mu.RLock()
	defer n.mu.RUnlock()
	if !n.prefs.Enabled {
		return false
	}
	if n.prefs.OnlyWhenUnfocused && n.focused {
		return false
	}
	switch eventType {
	case "message.received":
		return n.prefs.PrivateMessages || n.prefs.Mentions
	case "connection.lost":
		return n.prefs.ConnectionLost
	default:
		return false
	}
}

// IsPrivateMessage checks if the event data represents a private message
// (target is not a channel, i.e. does not start with # or &).
func IsPrivateMessage(data map[string]interface{}) bool {
	channel, ok := data["channel"].(string)
	if !ok || channel == "" {
		return false
	}
	return channel[0] != '#' && channel[0] != '&'
}

// IsMention checks if the message text contains the given nickname (case-insensitive).
func IsMention(message, nickname string) bool {
	if nickname == "" {
		return false
	}
	return strings.Contains(strings.ToLower(message), strings.ToLower(nickname))
}
