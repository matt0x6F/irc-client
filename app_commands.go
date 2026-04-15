package main

import (
	"fmt"
	"strings"

	"github.com/matt0x6f/irc-client/internal/logger"
	"github.com/matt0x6f/irc-client/internal/storage"
	"time"
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

	// Handle slash commands
	if strings.HasPrefix(command, "/") {
		parts := strings.Fields(command[1:]) // Remove leading /
		if len(parts) == 0 {
			return fmt.Errorf("invalid command")
		}

		cmd := strings.ToUpper(parts[0])
		switch cmd {
		case "JOIN", "J":
			if len(parts) < 2 {
				return fmt.Errorf("usage: /join #channel [key]")
			}
			channelName := parts[1]
			// If channel has a key, we need to send raw JOIN command
			if len(parts) >= 3 {
				rawCmd := fmt.Sprintf("JOIN %s %s", channelName, parts[2])
				return client.SendRawCommand(rawCmd)
			}
			logger.Log.Info().Str("channel", channelName).Msg("Joining channel")
			if err := client.JoinChannel(channelName); err != nil {
				logger.Log.Error().Err(err).Str("channel", channelName).Msg("Error joining channel")
				return err
			}
			logger.Log.Info().Str("channel", channelName).Msg("Successfully sent JOIN command")
			return nil
		case "PART", "LEAVE":
			if len(parts) < 2 {
				return fmt.Errorf("usage: /part #channel [reason]")
			}
			channelName := parts[1]
			var rawCmd string
			if len(parts) >= 3 {
				reason := strings.Join(parts[2:], " ")
				rawCmd = fmt.Sprintf("PART %s :%s", channelName, reason)
			} else {
				rawCmd = fmt.Sprintf("PART %s", channelName)
			}
			// Use PartChannel for basic part, or send raw if there's a reason
			if len(parts) >= 3 {
				return client.SendRawCommand(rawCmd)
			}
			return client.PartChannel(channelName)
		case "MSG", "PRIVMSG", "M":
			if len(parts) < 3 {
				return fmt.Errorf("usage: /msg target message")
			}
			target := parts[1]
			message := strings.Join(parts[2:], " ")
			return client.SendMessage(target, message)
		case "NICK":
			if len(parts) < 2 {
				return fmt.Errorf("usage: /nick newnick")
			}
			return client.SendRawCommand(fmt.Sprintf("NICK %s", parts[1]))
		case "QUIT":
			reason := ""
			if len(parts) >= 2 {
				reason = strings.Join(parts[1:], " ")
			}
			if reason != "" {
				return client.SendRawCommand(fmt.Sprintf("QUIT :%s", reason))
			}
			return client.SendRawCommand("QUIT")
		case "AWAY":
			message := ""
			if len(parts) >= 2 {
				message = strings.Join(parts[1:], " ")
			}
			if message != "" {
				return client.SendRawCommand(fmt.Sprintf("AWAY :%s", message))
			}
			// Empty away message means unset away
			return client.SendRawCommand("AWAY")
		case "WHOIS":
			if len(parts) < 2 {
				return fmt.Errorf("usage: /whois nickname")
			}
			return client.SendRawCommand(fmt.Sprintf("WHOIS %s", parts[1]))
		case "WHOWAS":
			if len(parts) < 2 {
				return fmt.Errorf("usage: /whowas nickname")
			}
			return client.SendRawCommand(fmt.Sprintf("WHOWAS %s", parts[1]))
		case "ME", "ACTION":
			if len(parts) < 2 {
				return fmt.Errorf("usage: /me action text")
			}
			var target string
			var actionText string
			if len(parts) >= 2 {
				target = parts[1]
				if len(parts) >= 3 {
					actionText = strings.Join(parts[2:], " ")
				} else {
					return fmt.Errorf("usage: /me [channel|user] action text")
				}
			} else {
				return fmt.Errorf("usage: /me [channel|user] action text")
			}
			return client.SendRawCommand(fmt.Sprintf("PRIVMSG %s :\001ACTION %s\001", target, actionText))
		case "CTCP":
			if len(parts) < 3 {
				return fmt.Errorf("usage: /ctcp target command [args]")
			}
			target := parts[1]
			ctcpCommand := strings.ToUpper(parts[2])
			ctcpArgs := ""
			if len(parts) > 3 {
				ctcpArgs = strings.Join(parts[3:], " ")
			}
			return client.SendCTCPRequest(target, ctcpCommand, ctcpArgs)
		case "VERSION":
			if len(parts) < 2 {
				return fmt.Errorf("usage: /version target")
			}
			return client.SendCTCPRequest(parts[1], "VERSION", "")
		case "TIME":
			if len(parts) < 2 {
				return fmt.Errorf("usage: /time target")
			}
			return client.SendCTCPRequest(parts[1], "TIME", "")
		case "PING":
			if len(parts) < 2 {
				return fmt.Errorf("usage: /ping target [args]")
			}
			target := parts[1]
			pingArgs := ""
			if len(parts) > 2 {
				pingArgs = strings.Join(parts[2:], " ")
			}
			return client.SendCTCPRequest(target, "PING", pingArgs)
		case "CLIENTINFO":
			if len(parts) < 2 {
				return fmt.Errorf("usage: /clientinfo target")
			}
			return client.SendCTCPRequest(parts[1], "CLIENTINFO", "")
		case "TOPIC":
			if len(parts) < 2 {
				return fmt.Errorf("usage: /topic #channel [new topic]")
			}
			channelName := parts[1]
			if len(parts) >= 3 {
				topic := strings.Join(parts[2:], " ")
				return client.SendRawCommand(fmt.Sprintf("TOPIC %s :%s", channelName, topic))
			}
			return client.SendRawCommand(fmt.Sprintf("TOPIC %s", channelName))
		case "MODE":
			if len(parts) < 2 {
				return fmt.Errorf("usage: /mode target modes [args]")
			}
			target := parts[1]
			if len(parts) >= 3 {
				modeArgs := strings.Join(parts[2:], " ")
				return client.SendRawCommand(fmt.Sprintf("MODE %s %s", target, modeArgs))
			}
			return client.SendRawCommand(fmt.Sprintf("MODE %s", target))
		case "INVITE":
			if len(parts) < 3 {
				return fmt.Errorf("usage: /invite nickname #channel")
			}
			return client.SendRawCommand(fmt.Sprintf("INVITE %s %s", parts[1], parts[2]))
		case "KICK":
			if len(parts) < 3 {
				return fmt.Errorf("usage: /kick #channel nickname [reason]")
			}
			channelName := parts[1]
			nickname := parts[2]
			if len(parts) >= 4 {
				reason := strings.Join(parts[3:], " ")
				return client.SendRawCommand(fmt.Sprintf("KICK %s %s :%s", channelName, nickname, reason))
			}
			return client.SendRawCommand(fmt.Sprintf("KICK %s %s", channelName, nickname))
		case "BAN":
			if len(parts) < 3 {
				return fmt.Errorf("usage: /ban #channel mask")
			}
			return client.SendRawCommand(fmt.Sprintf("MODE %s +b %s", parts[1], parts[2]))
		case "UNBAN":
			if len(parts) < 3 {
				return fmt.Errorf("usage: /unban #channel mask")
			}
			return client.SendRawCommand(fmt.Sprintf("MODE %s -b %s", parts[1], parts[2]))
		case "OP", "HOP":
			if len(parts) < 3 {
				return fmt.Errorf("usage: /op #channel nickname")
			}
			return client.SendRawCommand(fmt.Sprintf("MODE %s +o %s", parts[1], parts[2]))
		case "DEOP", "DEHOP":
			if len(parts) < 3 {
				return fmt.Errorf("usage: /deop #channel nickname")
			}
			return client.SendRawCommand(fmt.Sprintf("MODE %s -o %s", parts[1], parts[2]))
		case "VOICE", "V":
			if len(parts) < 3 {
				return fmt.Errorf("usage: /voice #channel nickname")
			}
			return client.SendRawCommand(fmt.Sprintf("MODE %s +v %s", parts[1], parts[2]))
		case "DEVOICE", "DEV":
			if len(parts) < 3 {
				return fmt.Errorf("usage: /devoice #channel nickname")
			}
			return client.SendRawCommand(fmt.Sprintf("MODE %s -v %s", parts[1], parts[2]))
		case "LIST":
			if len(parts) >= 2 {
				args := strings.Join(parts[1:], " ")
				return client.SendRawCommand(fmt.Sprintf("LIST %s", args))
			}
			return client.SendRawCommand("LIST")
		case "NAMES":
			if len(parts) >= 2 {
				return client.SendRawCommand(fmt.Sprintf("NAMES %s", parts[1]))
			}
			return client.SendRawCommand("NAMES")
		case "NOTICE":
			if len(parts) < 3 {
				return fmt.Errorf("usage: /notice target message")
			}
			target := parts[1]
			message := strings.Join(parts[2:], " ")
			return client.SendRawCommand(fmt.Sprintf("NOTICE %s :%s", target, message))
		case "QUERY", "Q":
			if len(parts) < 2 {
				return fmt.Errorf("usage: /query nickname [message]")
			}
			nickname := parts[1]
			if len(parts) >= 3 {
				message := strings.Join(parts[2:], " ")
				return client.SendMessage(nickname, message)
			}
			// No message - just open the PM window
			network, err := a.storage.GetNetwork(networkID)
			if err != nil {
				return fmt.Errorf("network not found: %w", err)
			}
			_, err = a.storage.GetOrCreatePMConversation(networkID, nickname, network.Nickname)
			if err != nil {
				return fmt.Errorf("failed to create PM conversation: %w", err)
			}
			messages, err := a.storage.GetPrivateMessages(networkID, nickname, network.Nickname, 1)
			if err == nil && len(messages) == 0 {
				placeholderMsg := storage.Message{
					NetworkID:   networkID,
					ChannelID:   nil,
					User:        nickname,
					Message:     "",
					MessageType: "privmsg",
					Timestamp:   time.Now(),
					RawLine:     fmt.Sprintf("QUERY %s", nickname),
				}
				if err := a.storage.WriteMessageSync(placeholderMsg); err != nil {
					logger.Log.Warn().Err(err).Str("nickname", nickname).Msg("Failed to store placeholder message for PM conversation")
				}
			}
			return a.SetPrivateMessageOpen(networkID, nickname, true)
		case "CLOSE":
			if len(parts) < 2 {
				return fmt.Errorf("usage: /close #channel or /close nickname")
			}
			target := parts[1]
			if len(target) > 0 && (target[0] == '#' || target[0] == '&') {
				return client.PartChannel(target)
			}
			return nil
		case "QUOTE", "RAW":
			if len(parts) < 2 {
				return fmt.Errorf("usage: /quote command [args] or /raw command [args]")
			}
			rawCommand := strings.Join(parts[1:], " ")
			return client.SendRawCommand(rawCommand)
		case "IGNORE":
			if len(parts) < 2 {
				return fmt.Errorf("usage: /ignore nickname")
			}
			return fmt.Errorf("/ignore is not yet implemented")
		case "UNIGNORE":
			if len(parts) < 2 {
				return fmt.Errorf("usage: /unignore nickname")
			}
			return fmt.Errorf("/unignore is not yet implemented")
		default:
			return client.SendRawCommand(command[1:])
		}
	}

	// Not a slash command, send as raw IRC command
	return client.SendRawCommand(command)
}
