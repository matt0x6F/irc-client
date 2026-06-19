package main

import (
	"fmt"
	"strings"

	"github.com/matt0x6f/irc-client/internal/irc"
)

// SendMessage sends a message to a channel or user
func (a *App) SendMessage(networkID int64, target, message string) error {
	a.mu.RLock()
	client, exists := a.ircClients[networkID]
	a.mu.RUnlock()

	if !exists {
		return fmt.Errorf("network not connected")
	}

	return client.SendMessage(target, message)
}

// RequestChatHistoryLatest asks the server to replay the most recent `limit`
// messages for target (channel name or PM nick). Used for on-open catch-up of a
// PM/query pane (channel catch-up fires automatically on JOIN). Replays are stored
// and deduped by msgid; the frontend learns of them via the "history-event".
func (a *App) RequestChatHistoryLatest(networkID int64, target string, limit int) error {
	a.mu.RLock()
	client, exists := a.ircClients[networkID]
	a.mu.RUnlock()

	if !exists {
		return fmt.Errorf("network not connected")
	}
	return client.RequestChatHistoryLatest(target, limit)
}

// RequestChatHistoryBefore asks the server to replay up to `limit` messages older
// than beforeISO (an ISO8601 timestamp) for target. Used by scroll-to-top deep
// backscroll once the local store is exhausted.
func (a *App) RequestChatHistoryBefore(networkID int64, target, beforeISO string, limit int) error {
	a.mu.RLock()
	client, exists := a.ircClients[networkID]
	a.mu.RUnlock()

	if !exists {
		return fmt.Errorf("network not connected")
	}
	return client.RequestChatHistoryBefore(target, beforeISO, limit)
}

// SendCommand sends a command from any channel or status window
// Supports commands like /join #channel, /msg user message, or raw IRC commands
func (a *App) SendCommand(networkID int64, command string) error {
	a.mu.RLock()
	client, exists := a.ircClients[networkID]
	a.mu.RUnlock()

	if !exists {
		return fmt.Errorf("network not connected")
	}

	// Parse command
	command = strings.TrimSpace(command)
	if len(command) == 0 {
		return fmt.Errorf("empty command")
	}

	if strings.HasPrefix(command, "/") {
		parts := strings.Fields(command[1:])
		if len(parts) == 0 {
			return fmt.Errorf("invalid command")
		}
		// Pass the original remainder (command without the leading slash) so the
		// unknown-command fallback preserves exact spacing/colons verbatim.
		return a.dispatchCommand(client, networkID, parts[0], parts[1:], command[1:])
	}

	return client.SendRawCommand(command)
}

// dispatchCommand routes a parsed slash command: built-in handler, then plugin
// command (Phase 4), then raw passthrough for unknown commands. rawRemainder is
// the original command text with the leading slash removed, used verbatim for
// the passthrough so multi-space/colon payloads are not mangled.
func (a *App) dispatchCommand(client *irc.IRCClient, networkID int64, name string, args []string, rawRemainder string) error {
	if spec, ok := a.commands.Lookup(name); ok {
		if spec.Frontend {
			return nil // handled in the frontend; should not reach here
		}
		if len(args) < spec.MinArgs {
			return fmt.Errorf("usage: /%s %s", strings.ToLower(spec.Name), spec.Usage)
		}
		return spec.handler(a, client, networkID, args)
	}
	// Unknown command: raw passthrough (preserves server-extension commands).
	return client.SendRawCommand(rawRemainder)
}
