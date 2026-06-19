package main

import (
	"time"

	"github.com/matt0x6f/irc-client/internal/irc"
	"github.com/matt0x6f/irc-client/internal/storage"
)

// PrintLocalLines writes client-local system lines (e.g. /help output) into a
// buffer through the normal message pipeline so they render and scroll like
// other system notices. target: "" or "status" => status buffer; "#name" =>
// channel; otherwise a PM peer nick.
func (a *App) PrintLocalLines(networkID int64, target string, lines []string) error {
	if len(lines) == 0 {
		return nil
	}

	var channelID *int64
	pmTarget := ""
	if target != "" && target != "status" {
		if target[0] == '#' || target[0] == '&' {
			if ch, err := a.storage.GetChannelByName(networkID, target); err == nil {
				channelID = &ch.ID
			}
		} else {
			pmTarget = target
		}
	}

	for _, line := range lines {
		msg := storage.Message{
			NetworkID:   networkID,
			ChannelID:   channelID,
			User:        "*",
			Message:     line,
			MessageType: "system",
			Timestamp:   time.Now(),
			RawLine:     line,
			PMTarget:    pmTarget,
		}
		if err := a.storage.WriteMessageSync(msg); err != nil {
			return err
		}
	}

	a.emit("message-event", map[string]interface{}{
		"type":      string(irc.EventMessageReceived),
		"data":      map[string]interface{}{"networkId": networkID, "channel": target},
		"timestamp": time.Now().Format(time.RFC3339),
	})
	return nil
}
