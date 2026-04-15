package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/matt0x6f/irc-client/internal/events"
	"github.com/matt0x6f/irc-client/internal/irc"
	"github.com/matt0x6f/irc-client/internal/logger"
	"github.com/matt0x6f/irc-client/internal/notification"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// OnEvent implements the events.Subscriber interface to forward events to frontend
func (a *App) OnEvent(event events.Event) {
	if a.ctx == nil {
		logger.Log.Debug().Msg("OnEvent: context not yet initialized, skipping event")
		return
	}

	// Handle connection status events
	if event.Type == irc.EventConnectionEstablished || event.Type == irc.EventConnectionLost {
		networkID, found := a.resolveNetworkID(event.Data)
		if found {
			isConnected := event.Type == irc.EventConnectionEstablished
			runtime.EventsEmit(a.ctx, "connection-status", map[string]interface{}{
				"networkId": networkID,
				"connected": isConnected,
				"timestamp": event.Timestamp.Format(time.RFC3339),
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

	// Handle channels changed event
	if event.Type == irc.EventChannelsChanged {
		networkID, ok := event.Data["networkId"].(int64)
		if !ok {
			if networkAddress, ok := event.Data["network"].(string); ok {
				networkID, _ = a.findNetworkIDByAddress(networkAddress)
			}
		}

		runtime.EventsEmit(a.ctx, "channels-changed", map[string]interface{}{
			"networkId": networkID,
			"timestamp": event.Timestamp.Format(time.RFC3339),
		})
		return
	}

	// Forward message and user events to frontend for real-time updates
	if event.Type == irc.EventMessageSent || event.Type == irc.EventMessageReceived ||
		event.Type == irc.EventUserJoined || event.Type == irc.EventUserParted || event.Type == irc.EventUserQuit ||
		event.Type == irc.EventUserKicked ||
		event.Type == irc.EventChannelTopic || event.Type == irc.EventChannelMode ||
		event.Type == irc.EventError || event.Type == "channel.names.complete" {
		runtime.EventsEmit(a.ctx, "message-event", map[string]interface{}{
			"type":      event.Type,
			"data":      event.Data,
			"timestamp": event.Timestamp.Format(time.RFC3339),
		})
	}

	// Forward channel list events to frontend
	if event.Type == irc.EventChannelListEnd {
		runtime.EventsEmit(a.ctx, "channel-list", map[string]interface{}{
			"type":      event.Type,
			"data":      event.Data,
			"timestamp": event.Timestamp.Format(time.RFC3339),
		})
	}

	// Forward WHOIS events to frontend
	if event.Type == irc.EventWhoisReceived {
		runtime.EventsEmit(a.ctx, "whois-event", map[string]interface{}{
			"type":      event.Type,
			"data":      event.Data,
			"timestamp": event.Timestamp.Format(time.RFC3339),
		})
	}

	// Handle metadata updates
	if event.Type == events.EventMetadataUpdated {
		runtime.EventsEmit(a.ctx, "metadata-updated", map[string]interface{}{
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
		runtime.EventsEmit(a.ctx, "ui-pane-event", map[string]interface{}{
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

		if notification.IsPrivateMessage(event.Data) {
			// Private message
			title := fmt.Sprintf("PM from %s", user)
			a.sendNotification(title, message)
		} else if notification.IsMention(message, myNick) {
			// Mention in a channel
			title := fmt.Sprintf("Mention in %s", channel)
			a.sendNotification(title, fmt.Sprintf("%s: %s", user, message))
		}

	case irc.EventConnectionLost:
		networkAddress, _ := event.Data["network"].(string)
		title := fmt.Sprintf("Connection Lost: %s", networkAddress)
		a.sendNotification(title, "Cascade Chat lost connection to the server.")
	}
}

// sendNotification is a helper that dispatches a desktop notification.
func (a *App) sendNotification(title, message string) {
	go func() {
		if err := a.notifier.Notify(title, message, ""); err != nil {
			logger.Log.Debug().Err(err).Str("title", title).Msg("Desktop notification failed")
		}
	}()
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
