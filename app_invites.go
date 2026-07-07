package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/matt0x6f/irc-client/internal/events"
	"github.com/matt0x6f/irc-client/internal/irc"
	"github.com/matt0x6f/irc-client/internal/logger"
	"github.com/matt0x6f/irc-client/internal/notification"
	"github.com/matt0x6f/irc-client/internal/storage"
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
	rows, err := a.storage.ListInviteActivity(networkID, time.Now())
	if err != nil {
		return nil, err
	}
	out := make([]InviteView, 0, len(rows))
	for _, r := range rows {
		out = append(out, InviteView{
			Inviter:    r.Actor,
			Channel:    r.Target,
			Trusted:    r.Trusted,
			ReceivedAt: r.Timestamp.Format(time.RFC3339),
		})
	}
	return out, nil
}

// DismissInvite removes a single (sender, channel) invite.
func (a *App) DismissInvite(networkID int64, inviter, channel string) error {
	if err := a.storage.DeleteInviteActivity(networkID, inviter, channel); err != nil {
		return err
	}
	a.emitInvitesChanged(networkID)
	a.emit("activity-changed")
	return nil
}

// DismissInvitesFrom removes all invites from a sender (without blocking them).
func (a *App) DismissInvitesFrom(networkID int64, inviter string) error {
	if err := a.storage.DeleteInviteActivityFromSender(networkID, inviter); err != nil {
		return err
	}
	a.emitInvitesChanged(networkID)
	a.emit("activity-changed")
	return nil
}

// emitInvitesChanged tells the frontend to reload a network's invites + badge.
func (a *App) emitInvitesChanged(networkID int64) {
	a.emit("invites.changed", map[string]any{"networkId": networkID})
}

// sweepInvitesOnce drops expired invite rows and notifies the frontend for each
// affected network. Extracted from the ticker loop so it is unit-testable.
func (a *App) sweepInvitesOnce() {
	nets, err := a.storage.SweepExpiredInvites(time.Now())
	if err != nil {
		logger.Log.Warn().Err(err).Msg("invite sweep failed")
		return
	}
	for _, net := range nets {
		a.emitInvitesChanged(net)
	}
	if len(nets) > 0 {
		a.emit("activity-changed")
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

// handleInviteReceived stores an inbound invite as a durable activity_items row,
// classifies trust against the buddy list, emits the change, and fires a
// desktop notification when warranted.
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
	if ignored, err := a.storage.IsSenderIgnored(networkID, inviter); err == nil && ignored {
		return
	}
	settings, err := a.GetActivitySettings()
	if err == nil && !settings.Invites {
		return
	}

	trusted := a.isBuddy(networkID, inviter)
	now := time.Now()
	item := storage.ActivityItem{
		NetworkID:  networkID,
		SourceType: string(irc.ActivityInvite),
		Target:     channel,
		Actor:      inviter,
		Preview:    fmt.Sprintf("%s invited you to %s", inviter, channel),
		Trusted:    trusted,
		Timestamp:  now,
	}
	if ttl := a.inviteTTL(); ttl > 0 {
		exp := now.Add(ttl)
		item.ExpiresAt = &exp
	}
	// dedup: drop any existing invite for the same (inviter, channel) first
	_ = a.storage.DeleteInviteActivity(networkID, inviter, channel)
	if _, err := a.storage.WriteActivityItem(item); err != nil {
		logger.Log.Warn().Err(err).Msg("failed to write invite activity")
		return
	}
	a.emitInvitesChanged(networkID)
	a.emit("activity-changed")

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
