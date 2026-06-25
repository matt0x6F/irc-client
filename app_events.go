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
)

// OnEvent implements the events.Subscriber interface to forward events to frontend
func (a *App) OnEvent(event events.Event) {
	if a.app == nil {
		logger.Log.Debug().Msg("OnEvent: application not yet initialized, skipping event")
		return
	}

	// Handle connection status events
	if event.Type == irc.EventConnectionEstablished || event.Type == irc.EventConnectionLost {
		networkID, found := a.resolveNetworkID(event.Data)
		if found {
			isConnected := event.Type == irc.EventConnectionEstablished
			a.emit("connection-status", map[string]interface{}{
				"networkId": networkID,
				"connected": isConnected,
				"timestamp": event.Timestamp.Format(time.RFC3339Nano),
			})

			if !isConnected {
				a.handleConnectionLost(networkID)
			}
		} else {
			logger.Log.Warn().
				Str("event_type", event.Type).
				Interface("event_data", event.Data).
				Msg("Could not determine network ID for connection event")
		}
		return
	}

	// Handle IRCv3 STS policy advertisements (plaintext→TLS upgrade or trusted persist)
	if event.Type == irc.EventSTSPolicy {
		a.handleSTSPolicy(event)
		return
	}

	// Handle our own nick changing (collision resolved, preferred nick reclaimed,
	// or a manual /nick). The frontend uses this to show the real current nick.
	if event.Type == irc.EventNickChanged {
		networkID, found := a.resolveNetworkID(event.Data)
		if found {
			a.emit("current-nick", map[string]interface{}{
				"networkId":   networkID,
				"nick":        event.Data["nick"],
				"desired":     event.Data["desired"],
				"isPreferred": event.Data["isPreferred"],
				"timestamp":   event.Timestamp.Format(time.RFC3339),
			})
		} else {
			logger.Log.Warn().
				Str("event_type", event.Type).
				Interface("event_data", event.Data).
				Msg("Could not determine network ID for nick-changed event")
		}
		return
	}

	// Handle channels changed event
	if event.Type == irc.EventChannelsChanged {
		networkID, ok := event.Data["networkId"].(int64)
		if !ok {
			if networkAddress, ok := event.Data["network"].(string); ok {
				networkID, _ = a.findNetworkIDByAddress(networkAddress)
			}
		}

		a.emit("channels-changed", map[string]interface{}{
			"networkId": networkID,
			"timestamp": event.Timestamp.Format(time.RFC3339),
		})
		return
	}

	// Forward message and user events to frontend for real-time updates
	if event.Type == irc.EventMessageSent || event.Type == irc.EventMessageReceived ||
		event.Type == irc.EventUserJoined || event.Type == irc.EventUserParted || event.Type == irc.EventUserQuit ||
		event.Type == irc.EventUserKicked || event.Type == irc.EventUserNick ||
		event.Type == irc.EventChannelTopic || event.Type == irc.EventChannelMode ||
		event.Type == irc.EventChannelUserMode || event.Type == irc.EventChannelBanList ||
		event.Type == irc.EventError || event.Type == "channel.names.complete" {
		a.emit("message-event", map[string]interface{}{
			"type":      event.Type,
			"data":      event.Data,
			"timestamp": event.Timestamp.Format(time.RFC3339),
		})
	}

	// Forward CHATHISTORY completion to the frontend so it can re-query the local
	// store (now backfilled) and decide whether to stop paging. Carries the target
	// and the count of newly-inserted rows.
	if event.Type == irc.EventHistoryReceived {
		a.emit("history-event", map[string]interface{}{
			"type":      event.Type,
			"data":      event.Data,
			"timestamp": event.Timestamp.Format(time.RFC3339),
		})
		return
	}

	// Forward IRCv3 +typing client tags to the frontend typing store. Ephemeral and
	// best-effort — no persistence, and the store self-expires stale entries.
	if event.Type == irc.EventTypingReceived {
		a.emit("typing-event", map[string]interface{}{
			"networkId": event.Data["networkId"],
			"target":    event.Data["target"],
			"nick":      event.Data["nick"],
			"state":     event.Data["state"],
		})
		return
	}

	// Forward channel list events to frontend, caching the result first so that
	// reopening the modal can render instantly without a fresh LIST. This runs after
	// the IRC 323 handler has cleared its accumulation buffer, so it is race-free.
	if event.Type == irc.EventChannelListEnd {
		if networkID, ok := event.Data["networkId"].(int64); ok {
			a.cacheChannelList(networkID, channelsFromEventData(event.Data["channels"]))
		}
		a.emit("channel-list", map[string]interface{}{
			"type":      event.Type,
			"data":      event.Data,
			"timestamp": event.Timestamp.Format(time.RFC3339),
		})
	}

	// Forward WHOIS events to frontend
	if event.Type == irc.EventWhoisReceived {
		a.emit("whois-event", map[string]interface{}{
			"type":      event.Type,
			"data":      event.Data,
			"timestamp": event.Timestamp.Format(time.RFC3339),
		})
	}

	// Forward bot-detected events to frontend (IRCv3 bot mode)
	if event.Type == irc.EventBotDetected {
		a.emit("bot-event", map[string]interface{}{
			"type":      event.Type,
			"data":      event.Data,
			"timestamp": event.Timestamp.Format(time.RFC3339),
		})
	}

	// Forward MONITOR presence changes to frontend (buddy-list online/offline)
	if event.Type == irc.EventMonitorChanged {
		a.emit("monitor-event", map[string]interface{}{
			"type":      event.Type,
			"data":      event.Data,
			"timestamp": event.Timestamp.Format(time.RFC3339),
		})
	}

	// Forward live-roster metadata changes to frontend (away-notify /
	// account-notify / extended-join / chghost / account-tag)
	if event.Type == irc.EventUserMetaChanged {
		a.emit("usermeta-event", map[string]interface{}{
			"type":      event.Type,
			"data":      event.Data,
			"timestamp": event.Timestamp.Format(time.RFC3339),
		})
	}

	// Forward plugin lifecycle changes so the frontend can refetch GetCommands()
	if event.Type == events.EventPluginLifecycle {
		a.emit("plugin-lifecycle", map[string]interface{}{
			"action": event.Data["action"],
			"plugin": event.Data["plugin"],
		})
		return
	}

	// Handle metadata updates
	if event.Type == events.EventMetadataUpdated {
		a.emit("metadata-updated", map[string]interface{}{
			"type":       event.Data["type"],
			"network_id": event.Data["network_id"],
			"channel":    event.Data["channel"],
			"key":        event.Data["key"],
			"value":      event.Data["value"],
		})
		return
	}

	// Forward UI pane events
	if event.Type == events.EventUIPaneFocused || event.Type == events.EventUIPaneBlurred {
		a.emit("ui-pane-event", map[string]interface{}{
			"type":      event.Type,
			"networkId": event.Data["networkId"],
			"paneType":  event.Data["type"],
			"paneName":  event.Data["name"],
			"timestamp": event.Timestamp.Format(time.RFC3339),
		})
		return
	}

	// Desktop notifications
	a.handleDesktopNotification(event)
}

// handleSTSPolicy reacts to an IRCv3 STS advertisement the client read from CAP LS.
// The transport the policy arrived over decides how much to trust it:
//
//   - over plaintext (secure=false): only the port is actionable. We record a pending
//     in-session upgrade and reconnect over TLS, but persist nothing — a MITM could
//     have injected the advertisement, so it must not outlive the session.
//   - over TLS (secure=true): the duration is trusted. We persist a durable policy
//     (duration=0 removes it instead) so every future connection to the host is forced
//     to TLS, then hand off from the in-memory pending upgrade to the stored policy.
func (a *App) handleSTSPolicy(event events.Event) {
	networkID, found := a.resolveNetworkID(event.Data)
	if !found {
		logger.Log.Warn().Interface("event_data", event.Data).Msg("Could not determine network ID for STS event")
		return
	}

	host, _ := event.Data["host"].(string)
	if host == "" {
		return
	}
	secure, _ := event.Data["secure"].(bool)
	port, _ := event.Data["port"].(int)
	currentPort, _ := event.Data["currentPort"].(int)
	duration, _ := event.Data["duration"].(int64)

	if !secure {
		// Plaintext: honor only the port directive, as an upgrade target. Persist nothing.
		if port <= 0 {
			return
		}
		a.mu.Lock()
		a.stsUpgrades[networkID] = stsTarget{host: host, port: port}
		a.mu.Unlock()
		a.emit("sts-policy", map[string]interface{}{"host": host, "active": true, "pending": true})
		go a.upgradeToTLS(networkID)
		return
	}

	// TLS: trusted. duration=0 means "forget this host".
	if duration <= 0 {
		if err := a.storage.DeleteSTSPolicy(host); err != nil {
			logger.Log.Warn().Err(err).Str("host", host).Msg("Failed to delete STS policy")
		}
		a.clearPendingSTSUpgrade(networkID)
		a.emit("sts-policy", map[string]interface{}{"host": host, "active": false})
		return
	}

	// The policy's secure port is the advertised port, falling back to the port we're
	// currently connected on (the spec makes port mandatory for the plaintext bootstrap
	// but optional over TLS, where we're already on a good port).
	securePort := port
	if securePort <= 0 {
		securePort = currentPort
	}
	expiresAt := time.Now().Unix() + duration
	if err := a.storage.UpsertSTSPolicy(host, securePort, expiresAt); err != nil {
		logger.Log.Error().Err(err).Str("host", host).Msg("Failed to persist STS policy")
		return
	}
	a.clearPendingSTSUpgrade(networkID)
	a.emit("sts-policy", map[string]interface{}{
		"host":      host,
		"active":    true,
		"port":      securePort,
		"expiresAt": expiresAt,
	})
}

// clearPendingSTSUpgrade drops the in-memory plaintext→TLS upgrade once a durable
// policy has taken over (or the host cleared its policy).
func (a *App) clearPendingSTSUpgrade(networkID int64) {
	a.mu.Lock()
	delete(a.stsUpgrades, networkID)
	a.mu.Unlock()
}

// handleDesktopNotification checks incoming events and sends desktop notifications
// for private messages, mentions, and connection lost events.
func (a *App) handleDesktopNotification(event events.Event) {
	if a.notifier == nil {
		return
	}

	if !a.notifier.ShouldNotify(event.Type, event.Data) {
		return
	}

	switch event.Type {
	case irc.EventMessageReceived:
		user, _ := event.Data["user"].(string)
		channel, _ := event.Data["channel"].(string)
		message, _ := event.Data["message"].(string)

		// Look up the network's current nickname to avoid self-notifications
		networkID, found := a.resolveNetworkID(event.Data)
		if !found {
			return
		}
		network, err := a.storage.GetNetwork(networkID)
		if err != nil {
			return
		}
		myNick := network.Nickname

		// Do not notify for messages from the current user
		if strings.EqualFold(user, myNick) {
			return
		}

		// Service/user notices (ChanServ, NickServ, …) route to a query pane but
		// must not raise a desktop notification per line — services are chatty,
		// especially on connect. They still badge the pane in-app.
		if mt, _ := event.Data["messageType"].(string); mt == "notice" {
			return
		}

		prefs := a.notifier.Prefs()
		if notification.IsPrivateMessage(event.Data) {
			if !prefs.PrivateMessages {
				return
			}
			a.sendNotification(notification.Notification{
				ID:         newNotificationID(),
				Title:      fmt.Sprintf("PM from %s", user),
				Body:       message,
				CategoryID: notifyCategoryMessage,
				Data: map[string]any{
					"networkId": strconv.FormatInt(networkID, 10),
					"target":    "pm:" + user,
					"kind":      "pm",
				},
			})
		} else if notification.IsMention(message, myNick) {
			if !prefs.Mentions {
				return
			}
			a.sendNotification(notification.Notification{
				ID:         newNotificationID(),
				Title:      fmt.Sprintf("Mention in %s", channel),
				Body:       fmt.Sprintf("%s: %s", user, message),
				CategoryID: notifyCategoryMessage,
				Data: map[string]any{
					"networkId": strconv.FormatInt(networkID, 10),
					"target":    channel,
					"kind":      "mention",
				},
			})
		}

	case irc.EventConnectionLost:
		networkAddress, _ := event.Data["network"].(string)
		networkID, _ := event.Data["networkId"].(int64)
		a.sendNotification(notification.Notification{
			ID:         newNotificationID(),
			Title:      fmt.Sprintf("Connection Lost: %s", networkAddress),
			Body:       "Cascade Chat lost connection to the server.",
			CategoryID: notifyCategoryConnection,
			Data: map[string]any{
				"networkId": strconv.FormatInt(networkID, 10),
				"target":    "",
				"kind":      "connection",
			},
		})
	}
}

// sendNotification dispatches an interactive desktop notification off the hot path.
func (a *App) sendNotification(n notification.Notification) {
	go a.notifier.Send(n)
}

// processPluginActions processes actions from plugins
func (a *App) processPluginActions() {
	for action := range a.pluginManager.GetActionQueue() {
		switch action.Type {
		case "send_message":
			serverAddr, _ := action.Data["server"].(string)
			target, _ := action.Data["target"].(string)
			message, _ := action.Data["message"].(string)

			a.mu.RLock()
			var client *irc.IRCClient
			if serverAddr != "" {
				networks, err := a.storage.GetNetworks()
				if err == nil {
					for _, network := range networks {
						if network.Address == serverAddr {
							if c, exists := a.ircClients[network.ID]; exists && c != nil {
								client = c
								break
							}
						}
						servers, err := a.storage.GetServers(network.ID)
						if err == nil {
							for _, srv := range servers {
								if srv.Address == serverAddr {
									if c, exists := a.ircClients[network.ID]; exists && c != nil {
										client = c
										break
									}
								}
							}
						}
						if client != nil {
							break
						}
					}
				}
			} else {
				for _, c := range a.ircClients {
					if c != nil {
						client = c
						break
					}
				}
			}
			a.mu.RUnlock()

			if client != nil {
				if err := client.SendMessage(target, message); err != nil {
					logger.Log.Error().Err(err).Str("target", target).Msg("Failed to send message from plugin")
				}
			} else {
				logger.Log.Warn().Str("server", serverAddr).Msg("No IRC client found for plugin action")
			}
		}
	}
}
