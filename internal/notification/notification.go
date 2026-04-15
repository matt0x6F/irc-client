package notification

import (
	"fmt"
	"strings"
	"sync"

	"github.com/gen2brain/beeep"
	"github.com/matt0x6f/irc-client/internal/logger"
)

// Notifier handles desktop notifications for the IRC client.
type Notifier struct {
	focused bool
	mu      sync.RWMutex
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

// Notify sends a desktop notification.
func (n *Notifier) Notify(title, message, appIcon string) error {
	if err := beeep.Notify(title, message, appIcon); err != nil {
		logger.Log.Warn().Err(err).Str("title", title).Msg("Failed to send desktop notification")
		return fmt.Errorf("failed to send notification: %w", err)
	}
	return nil
}

// ShouldNotify determines whether a notification should be sent based on the
// event type and data. It returns true for private messages, mentions, and
// connection lost events, but only when the window is not focused.
func (n *Notifier) ShouldNotify(eventType string, data map[string]interface{}) bool {
	if n.IsFocused() {
		return false
	}

	switch eventType {
	case "message.received":
		return true
	case "connection.lost":
		return true
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
