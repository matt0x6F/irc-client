package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/matt0x6f/irc-client/internal/events"
	"github.com/matt0x6f/irc-client/internal/notification"
)

// InviteView is one pending invite as seen by the frontend. ReceivedAt is an
// RFC3339 string — time.Time never crosses the Wails boundary.
type InviteView struct {
	Inviter    string `json:"inviter"`
	Channel    string `json:"channel"`
	Trusted    bool   `json:"trusted"`
	ReceivedAt string `json:"receivedAt"`
}

// inviteTTL reads the configurable invite lifetime (hours), defaulting to 24h.
func (a *App) inviteTTL() time.Duration {
	v, err := a.storage.GetSetting("invites.ttlHours")
	if err != nil || v == "" {
		return 24 * time.Hour
	}
	h, err := strconv.Atoi(v)
	if err != nil || h <= 0 {
		return 24 * time.Hour
	}
	return time.Duration(h) * time.Hour
}

// GetInvites returns the network's non-expired pending invites, newest-first.
func (a *App) GetInvites(networkID int64) ([]InviteView, error) {
	out := []InviteView{}
	for _, p := range a.invites.list(networkID) {
		out = append(out, InviteView{
			Inviter:    p.Inviter,
			Channel:    p.Channel,
			Trusted:    p.Trusted,
			ReceivedAt: p.ReceivedAt.Format(time.RFC3339),
		})
	}
	return out, nil
}

// DismissInvite removes a single (sender, channel) invite.
func (a *App) DismissInvite(networkID int64, inviter, channel string) error {
	a.invites.dismissOne(networkID, inviter, channel)
	a.emitInvitesChanged(networkID)
	return nil
}

// DismissInvitesFrom removes all invites from a sender (without blocking them).
func (a *App) DismissInvitesFrom(networkID int64, inviter string) error {
	a.invites.dismissFrom(networkID, inviter)
	a.emitInvitesChanged(networkID)
	return nil
}

// IgnoreInviteSender blocks a sender's invites for the session and removes any
// already pending. Session-only and invite-scoped (not a global /ignore).
func (a *App) IgnoreInviteSender(networkID int64, inviter string) error {
	a.invites.ignoreSender(networkID, inviter)
	a.emitInvitesChanged(networkID)
	return nil
}

// emitInvitesChanged tells the frontend to reload a network's invites + badge.
func (a *App) emitInvitesChanged(networkID int64) {
	a.emit("invites.changed", map[string]any{"networkId": networkID})
}

// sweepInvitesOnce drops expired invites and notifies the frontend for each
// affected network. Extracted from the ticker loop so it is unit-testable.
func (a *App) sweepInvitesOnce() {
	for _, net := range a.invites.sweep() {
		a.emitInvitesChanged(net)
	}
}

// startInviteSweeper runs sweepInvitesOnce on a 5-minute cadence until shutdown,
// so a buddy invite's badge clears on its own once it passes the TTL.
func (a *App) startInviteSweeper() {
	a.startupWg.Add(1)
	go func() {
		defer a.startupWg.Done()
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-a.startupCtx.Done():
				return
			case <-ticker.C:
				a.sweepInvitesOnce()
			}
		}
	}()
}

// handleInviteReceived stores an inbound invite, classifies trust against the
// buddy list, emits the change, and fires a desktop notification when warranted.
func (a *App) handleInviteReceived(event events.Event) {
	networkID, found := a.resolveNetworkID(event.Data)
	if !found {
		return
	}
	inviter, _ := event.Data["inviter"].(string)
	channel, _ := event.Data["channel"].(string)
	if inviter == "" || channel == "" {
		return
	}

	trusted := a.isBuddy(networkID, inviter)
	if !a.invites.add(networkID, inviter, channel, trusted) {
		return // blocked sender
	}
	a.emitInvitesChanged(networkID)

	if a.shouldNotifyInvite(trusted) {
		a.sendNotification(notification.Notification{
			ID:         newNotificationID(),
			Title:      fmt.Sprintf("Invite from %s", inviter),
			Body:       fmt.Sprintf("Invited you to %s", channel),
			CategoryID: notifyCategoryMessage,
			Data: map[string]any{
				"networkId": strconv.FormatInt(networkID, 10),
				"target":    "invites",
				"kind":      "invite",
			},
		})
	}
}

// isBuddy reports whether inviter is on the network's MONITOR/buddy list.
func (a *App) isBuddy(networkID int64, inviter string) bool {
	list, err := a.GetMonitorList(networkID)
	if err != nil {
		return false
	}
	for _, m := range list {
		if strings.EqualFold(m.Nick, inviter) {
			return true
		}
	}
	return false
}

// shouldNotifyInvite applies the attention-level setting and the global
// notification opt-in. Levels: "quiet" (never), "trusted" (default: buddies
// only), "all". A nil notifier never notifies.
func (a *App) shouldNotifyInvite(trusted bool) bool {
	if a.notifier == nil {
		return false
	}
	level, _ := a.storage.GetSetting("invites.attentionLevel")
	enabled := a.boolSetting("notifications.enabled", false)
	return inviteNotifyDecision(level, enabled, trusted)
}

// inviteNotifyDecision is the pure gating rule: notify only when notifications
// are enabled and the attention level admits this invite.
func inviteNotifyDecision(level string, enabled, trusted bool) bool {
	if !enabled {
		return false
	}
	switch level {
	case "quiet":
		return false
	case "all":
		return true
	default: // "trusted" and unset
		return trusted
	}
}
