package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/matt0x6f/irc-client/internal/irc"
	"github.com/matt0x6f/irc-client/internal/logger"
	"github.com/matt0x6f/irc-client/internal/storage"
)

// CommandCategory groups commands for help/autocomplete display.
type CommandCategory string

const (
	CategoryClient CommandCategory = "client" // interpreted by the client itself
	CategoryServer CommandCategory = "server" // IRC protocol verb sent to the server
	CategoryCTCP   CommandCategory = "ctcp"   // CTCP request
	CategoryPlugin CommandCategory = "plugin" // registered by a plugin
)

// HandlerFunc runs a built-in command. args is the argument list (the command
// word removed). The dispatcher has already resolved the client and enforced
// MinArgs, so handlers may assume len(args) >= MinArgs.
type HandlerFunc func(a *App, client *irc.IRCClient, networkID int64, args []string) error

// CommandSpec is the single source of truth for a command: behavior + metadata.
type CommandSpec struct {
	Name        string
	Aliases     []string
	Category    CommandCategory
	Usage       string // argument syntax only, e.g. "#channel [key]"
	Description string
	MinArgs     int
	Source      string // "" for built-in; plugin name otherwise
	Frontend    bool   // true => intercepted client-side; dispatch is a no-op
	handler     HandlerFunc
}

// CommandRegistry maps command names + aliases to specs.
type CommandRegistry struct {
	specs  []*CommandSpec
	byName map[string]*CommandSpec
}

func NewCommandRegistry() *CommandRegistry {
	return &CommandRegistry{byName: make(map[string]*CommandSpec)}
}

func (r *CommandRegistry) Register(spec *CommandSpec) {
	r.specs = append(r.specs, spec)
	for _, key := range append([]string{spec.Name}, spec.Aliases...) {
		k := strings.ToUpper(key)
		if _, dup := r.byName[k]; dup {
			panic("command registry: duplicate key " + k)
		}
		r.byName[k] = spec
	}
}

func (r *CommandRegistry) Lookup(name string) (*CommandSpec, bool) {
	spec, ok := r.byName[strings.ToUpper(name)]
	return spec, ok
}

func (r *CommandRegistry) Specs() []*CommandSpec {
	return r.specs
}

// buildBuiltinRegistry registers every built-in command. Usage strings are the
// argument portion only; the dispatcher prepends "usage: /<name> ".
func buildBuiltinRegistry() *CommandRegistry {
	r := NewCommandRegistry()
	reg := func(s *CommandSpec) { r.Register(s) }

	reg(&CommandSpec{Name: "JOIN", Aliases: []string{"J"}, Category: CategoryServer, Usage: "#channel [key]", Description: "Join a channel", MinArgs: 1, handler: cmdJoin})
	reg(&CommandSpec{Name: "PART", Aliases: []string{"LEAVE"}, Category: CategoryServer, Usage: "#channel [reason]", Description: "Leave a channel", MinArgs: 1, handler: cmdPart})
	reg(&CommandSpec{Name: "MSG", Aliases: []string{"PRIVMSG", "M"}, Category: CategoryServer, Usage: "target message", Description: "Send a message to a user or channel", MinArgs: 2, handler: cmdMsg})
	reg(&CommandSpec{Name: "NICK", Category: CategoryServer, Usage: "newnick", Description: "Change your nickname", MinArgs: 1, handler: cmdNick})
	reg(&CommandSpec{Name: "QUIT", Category: CategoryServer, Usage: "[reason]", Description: "Disconnect from the server", MinArgs: 0, handler: cmdQuit})
	reg(&CommandSpec{Name: "AWAY", Category: CategoryServer, Usage: "[message]", Description: "Set or clear your away status", MinArgs: 0, handler: cmdAway})
	reg(&CommandSpec{Name: "WHOIS", Category: CategoryServer, Usage: "nickname", Description: "Look up information about a user", MinArgs: 1, handler: cmdWhois})
	reg(&CommandSpec{Name: "WHOWAS", Category: CategoryServer, Usage: "nickname", Description: "Look up a user who has left", MinArgs: 1, handler: cmdWhowas})
	reg(&CommandSpec{Name: "ME", Aliases: []string{"ACTION"}, Category: CategoryServer, Usage: "[channel|user] action text", Description: "Send an action message", MinArgs: 2, handler: cmdMe})
	reg(&CommandSpec{Name: "CTCP", Category: CategoryCTCP, Usage: "target command [args]", Description: "Send a CTCP request", MinArgs: 2, handler: cmdCtcp})
	reg(&CommandSpec{Name: "VERSION", Category: CategoryCTCP, Usage: "target", Description: "Request a user's client version", MinArgs: 1, handler: cmdVersion})
	reg(&CommandSpec{Name: "TIME", Category: CategoryCTCP, Usage: "target", Description: "Request a user's local time", MinArgs: 1, handler: cmdTime})
	reg(&CommandSpec{Name: "PING", Category: CategoryCTCP, Usage: "target [args]", Description: "CTCP ping a user", MinArgs: 1, handler: cmdPing})
	reg(&CommandSpec{Name: "CLIENTINFO", Category: CategoryCTCP, Usage: "target", Description: "Request a user's supported CTCP commands", MinArgs: 1, handler: cmdClientinfo})
	reg(&CommandSpec{Name: "TOPIC", Category: CategoryServer, Usage: "#channel [new topic]", Description: "View or set a channel topic", MinArgs: 1, handler: cmdTopic})
	reg(&CommandSpec{Name: "MODE", Category: CategoryServer, Usage: "target modes [args]", Description: "View or change modes", MinArgs: 1, handler: cmdMode})
	reg(&CommandSpec{Name: "INVITE", Category: CategoryServer, Usage: "nickname #channel", Description: "Invite a user to a channel", MinArgs: 2, handler: cmdInvite})
	reg(&CommandSpec{Name: "KICK", Category: CategoryServer, Usage: "#channel nickname [reason]", Description: "Kick a user from a channel", MinArgs: 2, handler: cmdKick})
	reg(&CommandSpec{Name: "BAN", Category: CategoryServer, Usage: "#channel mask", Description: "Ban a mask from a channel", MinArgs: 2, handler: cmdBan})
	reg(&CommandSpec{Name: "UNBAN", Category: CategoryServer, Usage: "#channel mask", Description: "Remove a ban from a channel", MinArgs: 2, handler: cmdUnban})
	reg(&CommandSpec{Name: "OP", Aliases: []string{"HOP"}, Category: CategoryServer, Usage: "#channel nickname", Description: "Grant operator status", MinArgs: 2, handler: cmdOp})
	reg(&CommandSpec{Name: "DEOP", Aliases: []string{"DEHOP"}, Category: CategoryServer, Usage: "#channel nickname", Description: "Remove operator status", MinArgs: 2, handler: cmdDeop})
	reg(&CommandSpec{Name: "VOICE", Aliases: []string{"V"}, Category: CategoryServer, Usage: "#channel nickname", Description: "Grant voice", MinArgs: 2, handler: cmdVoice})
	reg(&CommandSpec{Name: "DEVOICE", Aliases: []string{"DEV"}, Category: CategoryServer, Usage: "#channel nickname", Description: "Remove voice", MinArgs: 2, handler: cmdDevoice})
	reg(&CommandSpec{Name: "LIST", Category: CategoryServer, Usage: "[args]", Description: "List channels", MinArgs: 0, handler: cmdList})
	reg(&CommandSpec{Name: "NAMES", Category: CategoryServer, Usage: "[#channel]", Description: "List users in a channel", MinArgs: 0, handler: cmdNames})
	reg(&CommandSpec{Name: "NOTICE", Category: CategoryServer, Usage: "target message", Description: "Send a notice", MinArgs: 2, handler: cmdNotice})
	reg(&CommandSpec{Name: "QUERY", Aliases: []string{"Q"}, Category: CategoryClient, Usage: "nickname [message]", Description: "Open a private conversation", MinArgs: 1, handler: cmdQuery})
	reg(&CommandSpec{Name: "CLOSE", Category: CategoryClient, Usage: "#channel or nickname", Description: "Close the current channel or query", MinArgs: 1, handler: cmdClose})
	reg(&CommandSpec{Name: "QUOTE", Aliases: []string{"RAW"}, Category: CategoryServer, Usage: "command [args]", Description: "Send a raw IRC command", MinArgs: 1, handler: cmdQuote})
	reg(&CommandSpec{Name: "IGNORE", Category: CategoryClient, Usage: "nickname", Description: "Ignore a user (not yet implemented)", MinArgs: 1, handler: cmdIgnore})
	reg(&CommandSpec{Name: "UNIGNORE", Category: CategoryClient, Usage: "nickname", Description: "Stop ignoring a user (not yet implemented)", MinArgs: 1, handler: cmdUnignore})

	// Frontend-handled: never dispatched to the backend (intercepted in the
	// store), but listed so it appears in autocomplete + help.
	reg(&CommandSpec{Name: "HELP", Category: CategoryClient, Usage: "[command]", Description: "Show available commands or help for one command", MinArgs: 0, Frontend: true})

	return r
}

func cmdJoin(a *App, client *irc.IRCClient, networkID int64, args []string) error {
	channelName := args[0]
	if len(args) >= 2 {
		return client.SendRawCommand(fmt.Sprintf("JOIN %s %s", channelName, args[1]))
	}
	logger.Log.Info().Str("channel", channelName).Msg("Joining channel")
	if err := client.JoinChannel(channelName); err != nil {
		logger.Log.Error().Err(err).Str("channel", channelName).Msg("Error joining channel")
		return err
	}
	return nil
}

func cmdPart(a *App, client *irc.IRCClient, networkID int64, args []string) error {
	channelName := args[0]
	if len(args) >= 2 {
		return client.SendRawCommand(fmt.Sprintf("PART %s :%s", channelName, strings.Join(args[1:], " ")))
	}
	return client.PartChannel(channelName)
}

func cmdMsg(a *App, client *irc.IRCClient, networkID int64, args []string) error {
	return client.SendMessage(args[0], strings.Join(args[1:], " "))
}

func cmdNick(a *App, client *irc.IRCClient, networkID int64, args []string) error {
	return client.ChangeNick(args[0])
}

func cmdQuit(a *App, client *irc.IRCClient, networkID int64, args []string) error {
	if len(args) >= 1 {
		return client.SendRawCommand(fmt.Sprintf("QUIT :%s", strings.Join(args, " ")))
	}
	return client.SendRawCommand("QUIT")
}

func cmdAway(a *App, client *irc.IRCClient, networkID int64, args []string) error {
	if len(args) >= 1 {
		return client.SendRawCommand(fmt.Sprintf("AWAY :%s", strings.Join(args, " ")))
	}
	return client.SendRawCommand("AWAY")
}

func cmdWhois(a *App, client *irc.IRCClient, networkID int64, args []string) error {
	return client.SendRawCommand(fmt.Sprintf("WHOIS %s", args[0]))
}

func cmdWhowas(a *App, client *irc.IRCClient, networkID int64, args []string) error {
	return client.SendRawCommand(fmt.Sprintf("WHOWAS %s", args[0]))
}

func cmdMe(a *App, client *irc.IRCClient, networkID int64, args []string) error {
	return client.SendRawCommand(fmt.Sprintf("PRIVMSG %s :\001ACTION %s\001", args[0], strings.Join(args[1:], " ")))
}

func cmdCtcp(a *App, client *irc.IRCClient, networkID int64, args []string) error {
	ctcpArgs := ""
	if len(args) > 2 {
		ctcpArgs = strings.Join(args[2:], " ")
	}
	return client.SendCTCPRequest(args[0], strings.ToUpper(args[1]), ctcpArgs)
}

func cmdVersion(a *App, client *irc.IRCClient, networkID int64, args []string) error {
	return client.SendCTCPRequest(args[0], "VERSION", "")
}

func cmdTime(a *App, client *irc.IRCClient, networkID int64, args []string) error {
	return client.SendCTCPRequest(args[0], "TIME", "")
}

func cmdPing(a *App, client *irc.IRCClient, networkID int64, args []string) error {
	pingArgs := ""
	if len(args) > 1 {
		pingArgs = strings.Join(args[1:], " ")
	}
	return client.SendCTCPRequest(args[0], "PING", pingArgs)
}

func cmdClientinfo(a *App, client *irc.IRCClient, networkID int64, args []string) error {
	return client.SendCTCPRequest(args[0], "CLIENTINFO", "")
}

func cmdTopic(a *App, client *irc.IRCClient, networkID int64, args []string) error {
	if len(args) >= 2 {
		return client.SendRawCommand(fmt.Sprintf("TOPIC %s :%s", args[0], strings.Join(args[1:], " ")))
	}
	return client.SendRawCommand(fmt.Sprintf("TOPIC %s", args[0]))
}

func cmdMode(a *App, client *irc.IRCClient, networkID int64, args []string) error {
	if len(args) >= 2 {
		return client.SendRawCommand(fmt.Sprintf("MODE %s %s", args[0], strings.Join(args[1:], " ")))
	}
	return client.SendRawCommand(fmt.Sprintf("MODE %s", args[0]))
}

func cmdInvite(a *App, client *irc.IRCClient, networkID int64, args []string) error {
	return client.SendRawCommand(fmt.Sprintf("INVITE %s %s", args[0], args[1]))
}

func cmdKick(a *App, client *irc.IRCClient, networkID int64, args []string) error {
	if len(args) >= 3 {
		return client.SendRawCommand(fmt.Sprintf("KICK %s %s :%s", args[0], args[1], strings.Join(args[2:], " ")))
	}
	return client.SendRawCommand(fmt.Sprintf("KICK %s %s", args[0], args[1]))
}

func cmdBan(a *App, client *irc.IRCClient, networkID int64, args []string) error {
	return client.SendRawCommand(fmt.Sprintf("MODE %s +b %s", args[0], args[1]))
}

func cmdUnban(a *App, client *irc.IRCClient, networkID int64, args []string) error {
	return client.SendRawCommand(fmt.Sprintf("MODE %s -b %s", args[0], args[1]))
}

func cmdOp(a *App, client *irc.IRCClient, networkID int64, args []string) error {
	return client.SendRawCommand(fmt.Sprintf("MODE %s +o %s", args[0], args[1]))
}

func cmdDeop(a *App, client *irc.IRCClient, networkID int64, args []string) error {
	return client.SendRawCommand(fmt.Sprintf("MODE %s -o %s", args[0], args[1]))
}

func cmdVoice(a *App, client *irc.IRCClient, networkID int64, args []string) error {
	return client.SendRawCommand(fmt.Sprintf("MODE %s +v %s", args[0], args[1]))
}

func cmdDevoice(a *App, client *irc.IRCClient, networkID int64, args []string) error {
	return client.SendRawCommand(fmt.Sprintf("MODE %s -v %s", args[0], args[1]))
}

func cmdList(a *App, client *irc.IRCClient, networkID int64, args []string) error {
	if len(args) >= 1 {
		return client.SendRawCommand(fmt.Sprintf("LIST %s", strings.Join(args, " ")))
	}
	return client.SendRawCommand("LIST")
}

func cmdNames(a *App, client *irc.IRCClient, networkID int64, args []string) error {
	if len(args) >= 1 {
		return client.SendRawCommand(fmt.Sprintf("NAMES %s", args[0]))
	}
	return client.SendRawCommand("NAMES")
}

func cmdNotice(a *App, client *irc.IRCClient, networkID int64, args []string) error {
	return client.SendRawCommand(fmt.Sprintf("NOTICE %s :%s", args[0], strings.Join(args[1:], " ")))
}

func cmdQuery(a *App, client *irc.IRCClient, networkID int64, args []string) error {
	nickname := args[0]
	if len(args) >= 2 {
		return client.SendMessage(nickname, strings.Join(args[1:], " "))
	}
	network, err := a.storage.GetNetwork(networkID)
	if err != nil {
		return fmt.Errorf("network not found: %w", err)
	}
	if _, err = a.storage.GetOrCreatePMConversation(networkID, nickname, network.Nickname); err != nil {
		return fmt.Errorf("failed to create PM conversation: %w", err)
	}
	messages, err := a.storage.GetPrivateMessages(networkID, nickname, network.Nickname, 1)
	if err == nil && len(messages) == 0 {
		placeholderMsg := storage.Message{
			NetworkID: networkID, ChannelID: nil, User: nickname, Message: "",
			MessageType: "privmsg", Timestamp: time.Now(),
			RawLine: fmt.Sprintf("QUERY %s", nickname), PMTarget: nickname,
		}
		if err := a.storage.WriteMessageSync(placeholderMsg); err != nil {
			logger.Log.Warn().Err(err).Str("nickname", nickname).Msg("Failed to store placeholder message for PM conversation")
		}
	}
	return a.SetPrivateMessageOpen(networkID, nickname, true)
}

func cmdClose(a *App, client *irc.IRCClient, networkID int64, args []string) error {
	target := args[0]
	if len(target) > 0 && (target[0] == '#' || target[0] == '&') {
		return client.PartChannel(target)
	}
	return nil
}

func cmdQuote(a *App, client *irc.IRCClient, networkID int64, args []string) error {
	return client.SendRawCommand(strings.Join(args, " "))
}

func cmdIgnore(a *App, client *irc.IRCClient, networkID int64, args []string) error {
	return fmt.Errorf("/ignore is not yet implemented")
}

func cmdUnignore(a *App, client *irc.IRCClient, networkID int64, args []string) error {
	return fmt.Errorf("/unignore is not yet implemented")
}

// CommandInfo is the wire/metadata view of a command for the frontend.
type CommandInfo struct {
	Name        string   `json:"name"`
	Aliases     []string `json:"aliases"`
	Category    string   `json:"category"`
	Usage       string   `json:"usage"`
	Description string   `json:"description"`
	Source      string   `json:"source"`
}

func specToInfo(s *CommandSpec) CommandInfo {
	return CommandInfo{
		Name: s.Name, Aliases: s.Aliases, Category: string(s.Category),
		Usage: s.Usage, Description: s.Description, Source: s.Source,
	}
}

// mergeCommandInfos produces the full command list: built-ins followed by plugin commands.
func mergeCommandInfos(r *CommandRegistry, plugin []CommandInfo) []CommandInfo {
	specs := r.Specs()
	out := make([]CommandInfo, 0, len(specs)+len(plugin))
	for _, s := range specs {
		out = append(out, specToInfo(s))
	}
	return append(out, plugin...)
}

// GetCommands returns metadata for every known command (built-ins merged with
// plugin commands). Bound to the frontend via Wails.
func (a *App) GetCommands() []CommandInfo {
	var plugin []CommandInfo
	if a.pluginManager != nil {
		for _, e := range a.pluginManager.PluginCommands() {
			plugin = append(plugin, CommandInfo{
				Name: strings.ToUpper(e.Spec.Name), Aliases: e.Spec.Aliases,
				Category: string(CategoryPlugin), Usage: e.Spec.Usage,
				Description: e.Spec.Description, Source: e.Plugin,
			})
		}
	}
	return mergeCommandInfos(a.commands, plugin)
}
