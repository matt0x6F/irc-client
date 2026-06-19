# Command Registry, Autocomplete, Help & DM Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give Cascade Chat a registry-driven command system: one `CommandSpec` source of truth that drives dispatch, slash-command autocomplete (popup + usage hint), a categorized application help surface (Client / Server / CTCP / Plugin), plugin-registered slash commands, and DM-on-double-click of a nick (with `/query` navigation).

**Architecture:** A Go command registry (`commands_registry.go`) replaces the 30-case `switch` in `SendCommand` with a `map[string]*CommandSpec` lookup; built-in behavior is preserved by extracting each case into a `HandlerFunc`. Unknown commands keep the raw-passthrough fallback. A new bound `GetCommands()` exposes registry metadata to the frontend, which drives autocomplete and help. Plugins declare `Commands` in their `initialize` response; the manager registers them into a parallel table consulted after built-ins, invoked over a new `command.invoke` RPC, with effects flowing back through the existing action queue (whose producer half we wire). A new `plugin.lifecycle` event tells the frontend to refetch the command list.

**Tech Stack:** Go (`ergochat/irc-go`, Wails v3, JSON-RPC plugin subprocesses), React 19 + Zustand + TypeScript, Vitest.

## Global Constraints

- Backend tests MUST run with the fts5 tag: `CGO_ENABLED=1 go test -tags fts5 ./... -count=1`.
- Frontend tests: `cd frontend && npx vitest run`.
- Durable user prefs go through `App.GetSetting`/`App.SetSetting` (SQLite settings table) — NEVER `localStorage` (WKWebView drops it across restarts).
- After changing any bound `App` method or model, regenerate Wails bindings: `wails3 generate bindings -clean=true` (committed bindings live at `frontend/wailsjs/`; model index renumber of `$$createType` is benign).
- All Wails-bound methods stay on the `App` struct in `package main`.
- Error wrapping uses `%w`. Shared App state is mutex-guarded.
- The raw-passthrough fallback for unknown `/commands` MUST be preserved.
- The switch→registry refactor MUST NOT change observable dispatch behavior (asserted by tests) before new features build on it.

Design spec: [docs/plans/2026-06-19-command-registry-help-autocomplete.md](2026-06-19-command-registry-help-autocomplete.md).

---

# Phase 1 — Go command registry + dispatch refactor + GetCommands

## Task 1: CommandSpec types and registry container

**Files:**
- Create: `commands_registry.go`
- Test: `commands_registry_test.go`

**Interfaces:**
- Produces:
  - `type CommandCategory string` with consts `CategoryClient="client"`, `CategoryServer="server"`, `CategoryCTCP="ctcp"`, `CategoryPlugin="plugin"`.
  - `type HandlerFunc func(a *App, client *irc.IRCClient, networkID int64, args []string) error`
  - `type CommandSpec struct { Name string; Aliases []string; Category CommandCategory; Usage string; Description string; MinArgs int; Source string; Frontend bool; handler HandlerFunc }`
  - `type CommandRegistry struct { specs []*CommandSpec; byName map[string]*CommandSpec }`
  - `func NewCommandRegistry() *CommandRegistry`
  - `func (r *CommandRegistry) Register(spec *CommandSpec)` — registers under upper-cased `Name` and each alias; panics on duplicate key (built-ins are static).
  - `func (r *CommandRegistry) Lookup(name string) (*CommandSpec, bool)` — upper-cases `name`.
  - `func (r *CommandRegistry) Specs() []*CommandSpec` — canonical specs in registration order (one per command, not per alias).

- [ ] **Step 1: Write the failing test**

```go
package main

import "testing"

func TestCommandRegistryRegisterAndLookup(t *testing.T) {
	r := NewCommandRegistry()
	spec := &CommandSpec{Name: "JOIN", Aliases: []string{"J"}, Category: CategoryServer, Usage: "#channel [key]", Description: "Join a channel", MinArgs: 1}
	r.Register(spec)

	for _, key := range []string{"JOIN", "join", "J", "j"} {
		got, ok := r.Lookup(key)
		if !ok || got != spec {
			t.Fatalf("Lookup(%q) = %v, %v; want the JOIN spec", key, got, ok)
		}
	}
	if _, ok := r.Lookup("NOPE"); ok {
		t.Fatalf("Lookup(NOPE) should miss")
	}
	if got := r.Specs(); len(got) != 1 {
		t.Fatalf("Specs() returned %d specs; want 1 canonical spec", len(got))
	}
}

func TestCommandRegistryDuplicatePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatalf("registering a duplicate key should panic")
		}
	}()
	r := NewCommandRegistry()
	r.Register(&CommandSpec{Name: "JOIN"})
	r.Register(&CommandSpec{Name: "JOIN"})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `CGO_ENABLED=1 go test -tags fts5 -run TestCommandRegistry . -count=1`
Expected: FAIL — undefined: `NewCommandRegistry`, `CommandSpec`, etc.

- [ ] **Step 3: Write minimal implementation**

```go
package main

import (
	"strings"

	"github.com/matt0x6f/irc-client/internal/irc"
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `CGO_ENABLED=1 go test -tags fts5 -run TestCommandRegistry . -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add commands_registry.go commands_registry_test.go
git commit -m "feat(commands): add CommandSpec registry container"
```

## Task 2: Built-in command handlers + spec table

This extracts every `switch` case from `app_commands.go` into a `HandlerFunc` and registers it. Behavior is preserved exactly; `Usage` strings are chosen so the dispatcher's generated `usage: /<name> <Usage>` error matches the current inline messages.

**Files:**
- Modify: `commands_registry.go`
- Test: `commands_registry_test.go`

**Interfaces:**
- Produces: `func buildBuiltinRegistry() *CommandRegistry` returning a registry with all built-ins registered.

- [ ] **Step 1: Write the failing test**

```go
func TestBuiltinRegistryCoverage(t *testing.T) {
	r := buildBuiltinRegistry()
	// Spot-check representative commands across categories + aliases.
	cases := map[string]CommandCategory{
		"JOIN": CategoryServer, "J": CategoryServer,
		"MSG": CategoryServer, "PRIVMSG": CategoryServer, "M": CategoryServer,
		"QUERY": CategoryClient, "Q": CategoryClient,
		"CLOSE": CategoryClient, "QUOTE": CategoryServer, "RAW": CategoryServer,
		"VERSION": CategoryCTCP, "CTCP": CategoryCTCP, "PING": CategoryCTCP,
		"OP": CategoryServer, "HOP": CategoryServer, "DEVOICE": CategoryServer,
		"HELP": CategoryClient,
	}
	for name, cat := range cases {
		spec, ok := r.Lookup(name)
		if !ok {
			t.Errorf("missing built-in %q", name)
			continue
		}
		if spec.Category != cat {
			t.Errorf("%q category = %q; want %q", name, spec.Category, cat)
		}
	}
	if spec, _ := r.Lookup("HELP"); spec == nil || !spec.Frontend {
		t.Errorf("HELP must be a frontend-handled command")
	}
	if spec, _ := r.Lookup("JOIN"); spec.Usage != "#channel [key]" || spec.MinArgs != 1 {
		t.Errorf("JOIN spec metadata wrong: %+v", spec)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `CGO_ENABLED=1 go test -tags fts5 -run TestBuiltinRegistryCoverage . -count=1`
Expected: FAIL — undefined: `buildBuiltinRegistry`

- [ ] **Step 3: Write the handlers + table**

Append to `commands_registry.go`. The handler bodies are the extracted `switch` case bodies with `parts[1]→args[0]`, `parts[2:]→args[1:]`, `parts[1:]→args`. Add `"fmt"`, `"strings"`, `"time"` and the storage/logger imports as needed.

```go
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
```

Add to the import block at the top of `commands_registry.go`:

```go
import (
	"fmt"
	"strings"
	"time"

	"github.com/matt0x6f/irc-client/internal/irc"
	"github.com/matt0x6f/irc-client/internal/logger"
	"github.com/matt0x6f/irc-client/internal/storage"
)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `CGO_ENABLED=1 go test -tags fts5 -run TestBuiltinRegistryCoverage . -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add commands_registry.go commands_registry_test.go
git commit -m "feat(commands): extract built-in handlers into registry specs"
```

## Task 3: Rewire SendCommand to dispatch through the registry

**Files:**
- Modify: `app.go` (add `commands *CommandRegistry` field to `App`; initialize in `NewApp`)
- Modify: `app_commands.go` (`SendCommand` body)
- Test: `app_commands_dispatch_test.go`

**Interfaces:**
- Consumes: `buildBuiltinRegistry()`, `CommandRegistry.Lookup`.
- Produces: `App.commands *CommandRegistry`; `SendCommand` routing: built-in handler → plugin (Phase 4, stubbed as miss for now) → raw passthrough.

- [ ] **Step 1: Write the failing test**

A `nil` IRC client never gets dereferenced for the cases under test: unknown commands and `MinArgs` failures are decided before the handler runs. Connect path is covered by existing integration tests; here we assert routing decisions.

```go
package main

import (
	"strings"
	"testing"
)

func TestSendCommandUsageError(t *testing.T) {
	a := &App{commands: buildBuiltinRegistry()}
	// JOIN requires 1 arg; expect the generated usage error, not a panic.
	err := a.dispatchCommand(nil, 1, "JOIN", nil, "JOIN")
	if err == nil || !strings.Contains(err.Error(), "usage: /join #channel [key]") {
		t.Fatalf("got %v; want JOIN usage error", err)
	}
}

func TestSendCommandFrontendNoOp(t *testing.T) {
	a := &App{commands: buildBuiltinRegistry()}
	if err := a.dispatchCommand(nil, 1, "HELP", nil, "HELP"); err != nil {
		t.Fatalf("HELP (frontend) dispatch should no-op, got %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `CGO_ENABLED=1 go test -tags fts5 -run TestSendCommand . -count=1`
Expected: FAIL — undefined: `App.commands`, `dispatchCommand`.

- [ ] **Step 3: Implement**

In `app.go`, add to the `App` struct:

```go
	commands *CommandRegistry
```

In `NewApp`, after the struct is built (near where `pluginManager` is set), set:

```go
	app.commands = buildBuiltinRegistry()
```

Replace the slash-command block of `SendCommand` in `app_commands.go` (lines 71-355, the `if strings.HasPrefix(command, "/")` block) with:

```go
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
```

Ensure `app_commands.go` imports include `"github.com/matt0x6f/irc-client/internal/irc"`. Remove now-unused imports from `app_commands.go` if `logger`/`storage`/`time` are no longer referenced there (they moved to `commands_registry.go`); run `goimports`/`go build` to confirm.

- [ ] **Step 4: Run tests**

Run: `CGO_ENABLED=1 go test -tags fts5 -run TestSendCommand . -count=1`
Expected: PASS

Run the full backend suite to confirm no regression:
Run: `CGO_ENABLED=1 go test -tags fts5 ./... -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add app.go app_commands.go app_commands_dispatch_test.go
git commit -m "refactor(commands): dispatch SendCommand through the registry"
```

## Task 4: GetCommands bound method + binding regen

**Files:**
- Modify: `app.go` (add `GetCommands` bound method + `CommandInfo` type) or add to `commands_registry.go`
- Test: `commands_registry_test.go`
- Regenerate: `frontend/wailsjs/`

**Interfaces:**
- Produces:
  - `type CommandInfo struct { Name string `json:"name"`; Aliases []string `json:"aliases"`; Category string `json:"category"`; Usage string `json:"usage"`; Description string `json:"description"`; Source string `json:"source"` }`
  - `func (a *App) GetCommands() []CommandInfo` — built-in specs now; merged with plugin specs in Phase 4.

- [ ] **Step 1: Write the failing test**

```go
func TestGetCommandsMetadata(t *testing.T) {
	a := &App{commands: buildBuiltinRegistry()}
	infos := a.GetCommands()
	if len(infos) == 0 {
		t.Fatal("GetCommands returned nothing")
	}
	var join *CommandInfo
	for i := range infos {
		if infos[i].Name == "JOIN" {
			join = &infos[i]
		}
	}
	if join == nil {
		t.Fatal("JOIN missing from GetCommands")
	}
	if join.Category != "server" || join.Usage != "#channel [key]" || len(join.Aliases) != 1 || join.Aliases[0] != "J" {
		t.Fatalf("JOIN info wrong: %+v", join)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `CGO_ENABLED=1 go test -tags fts5 -run TestGetCommandsMetadata . -count=1`
Expected: FAIL — undefined: `CommandInfo`, `GetCommands`.

- [ ] **Step 3: Implement** (append to `commands_registry.go`)

```go
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

// GetCommands returns metadata for every known command (built-in here; merged
// with plugin commands in Phase 4). Bound to the frontend via Wails.
func (a *App) GetCommands() []CommandInfo {
	specs := a.commands.Specs()
	infos := make([]CommandInfo, 0, len(specs))
	for _, s := range specs {
		infos = append(infos, specToInfo(s))
	}
	return infos
}
```

- [ ] **Step 4: Run test + regenerate bindings**

Run: `CGO_ENABLED=1 go test -tags fts5 -run TestGetCommandsMetadata . -count=1`
Expected: PASS

Run: `wails3 generate bindings -clean=true`
Expected: `frontend/wailsjs/go/main/App.{js,d.ts}` gains `GetCommands`; `models.ts` gains `main.CommandInfo`.

- [ ] **Step 5: Commit**

```bash
git add commands_registry.go commands_registry_test.go frontend/wailsjs
git commit -m "feat(commands): expose GetCommands binding for the frontend"
```

---

# Phase 2 — Frontend autocomplete (popup + usage hint)

## Task 5: Commands store

**Files:**
- Create: `frontend/src/stores/commands.ts`
- Test: `frontend/src/stores/commands.test.ts`

**Interfaces:**
- Produces: `useCommandsStore` with `{ commands: main.CommandInfo[]; loadCommands(): Promise<void> }`; `initCommands()` to hydrate at startup; `filterCommands(commands, prefix): main.CommandInfo[]` pure helper (prefix without leading `/`, case-insensitive match on name + aliases, sorted, deduped to canonical name).

- [ ] **Step 1: Write the failing test**

```ts
import { describe, it, expect } from 'vitest';
import { filterCommands } from './commands';
import { main } from '../../wailsjs/go/models';

const mk = (name: string, aliases: string[] = [], category = 'server'): main.CommandInfo =>
  ({ name, aliases, category, usage: '', description: '', source: '' } as main.CommandInfo);

describe('filterCommands', () => {
  const cmds = [mk('JOIN', ['J']), mk('QUERY', ['Q']), mk('QUIT')];
  it('matches by name prefix, case-insensitive', () => {
    expect(filterCommands(cmds, 'qu').map(c => c.name)).toEqual(['QUERY', 'QUIT']);
  });
  it('matches aliases', () => {
    expect(filterCommands(cmds, 'j').map(c => c.name)).toEqual(['JOIN']);
  });
  it('returns all on empty prefix', () => {
    expect(filterCommands(cmds, '').length).toBe(3);
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && npx vitest run src/stores/commands.test.ts`
Expected: FAIL — cannot import `filterCommands`.

- [ ] **Step 3: Implement**

```ts
import { create } from 'zustand';
import { GetCommands } from '../../wailsjs/go/main/App';
import { main } from '../../wailsjs/go/models';

interface CommandsState {
  commands: main.CommandInfo[];
  loadCommands: () => Promise<void>;
}

export const useCommandsStore = create<CommandsState>((set) => ({
  commands: [],
  loadCommands: async () => {
    try {
      const cmds = await GetCommands();
      set({ commands: Array.isArray(cmds) ? cmds : [] });
    } catch (error) {
      console.error('Failed to load commands:', error);
    }
  },
}));

export async function initCommands(): Promise<void> {
  await useCommandsStore.getState().loadCommands();
}

/** Filter commands whose name or any alias starts with prefix (no leading slash). */
export function filterCommands(commands: main.CommandInfo[], prefix: string): main.CommandInfo[] {
  const p = prefix.toLowerCase();
  const matches = commands.filter(
    (c) =>
      c.name.toLowerCase().startsWith(p) ||
      (c.aliases || []).some((a) => a.toLowerCase().startsWith(p))
  );
  return matches.sort((a, b) => a.name.localeCompare(b.name));
}

/** Find the canonical spec for a typed command word (name or alias), or null. */
export function lookupCommand(commands: main.CommandInfo[], word: string): main.CommandInfo | null {
  const w = word.toLowerCase();
  return (
    commands.find(
      (c) => c.name.toLowerCase() === w || (c.aliases || []).some((a) => a.toLowerCase() === w)
    ) || null
  );
}
```

- [ ] **Step 4: Run test**

Run: `cd frontend && npx vitest run src/stores/commands.test.ts`
Expected: PASS

- [ ] **Step 5: Hydrate at startup + commit**

In `frontend/src/App.tsx`, import `initCommands` and call it once in the startup effect alongside the other init calls (e.g. near `initPreferences()` / `loadNetworks()`):

```ts
initCommands();
```

```bash
git add frontend/src/stores/commands.ts frontend/src/stores/commands.test.ts frontend/src/App.tsx
git commit -m "feat(commands): frontend commands store + filtering"
```

## Task 6: Command-line parsing helper

**Files:**
- Create: `frontend/src/lib/command-line.ts`
- Test: `frontend/src/lib/command-line.test.ts`

**Interfaces:**
- Produces: `parseCommandLine(text: string): { isCommand: boolean; word: string; afterCommandName: boolean }` — `isCommand` true when `text` starts with `/`; `word` is the command token (between `/` and first space); `afterCommandName` true once a space follows the command word (i.e. typing arguments).

- [ ] **Step 1: Write the failing test**

```ts
import { describe, it, expect } from 'vitest';
import { parseCommandLine } from './command-line';

describe('parseCommandLine', () => {
  it('detects a bare command being typed', () => {
    expect(parseCommandLine('/jo')).toEqual({ isCommand: true, word: 'jo', afterCommandName: false });
  });
  it('detects arguments started', () => {
    expect(parseCommandLine('/join ')).toEqual({ isCommand: true, word: 'join', afterCommandName: true });
  });
  it('is not a command without a leading slash', () => {
    expect(parseCommandLine('hello').isCommand).toBe(false);
  });
  it('ignores a lone slash mid-text', () => {
    expect(parseCommandLine('a/b').isCommand).toBe(false);
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && npx vitest run src/lib/command-line.test.ts`
Expected: FAIL — cannot import `parseCommandLine`.

- [ ] **Step 3: Implement**

```ts
export interface CommandLine {
  isCommand: boolean;
  word: string;
  afterCommandName: boolean;
}

/** Parse the composer text to decide command-autocomplete state. */
export function parseCommandLine(text: string): CommandLine {
  if (!text.startsWith('/')) {
    return { isCommand: false, word: '', afterCommandName: false };
  }
  const rest = text.slice(1);
  const spaceIdx = rest.indexOf(' ');
  if (spaceIdx === -1) {
    return { isCommand: true, word: rest, afterCommandName: false };
  }
  return { isCommand: true, word: rest.slice(0, spaceIdx), afterCommandName: true };
}
```

- [ ] **Step 4: Run test**

Run: `cd frontend && npx vitest run src/lib/command-line.test.ts`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add frontend/src/lib/command-line.ts frontend/src/lib/command-line.test.ts
git commit -m "feat(commands): command-line parsing helper"
```

## Task 7: Autocomplete popup + usage hint in InputArea

**Files:**
- Modify: `frontend/src/components/input-area.tsx`
- Test: `frontend/src/components/input-area.command.test.tsx`

**Interfaces:**
- Consumes: `useCommandsStore`, `filterCommands`, `lookupCommand`, `parseCommandLine`.
- Behavior: when `parseCommandLine(message).isCommand && !afterCommandName`, render a popup of `filterCommands(commands, word)` above the input; `↓/↑` move selection, `Enter`/`Tab` accept (replace text with `/<name> ` and keep focus), `Esc` dismiss. When `afterCommandName` and `lookupCommand` matches, render a usage hint bar (`/<name> <usage> — <description>`) above the input. Existing nick Tab-completion stays active only when the popup is NOT showing.

**Implementation notes (decision):** the "inline hint" is rendered as a one-line hint bar directly above the composer rather than a true ghost overlay inside the `<input>` — a single-line `<input>` cannot host overlaid ghost text reliably, and a hint bar is robust and equally discoverable. Popup selection and `Esc`/accept handling are added at the top of the existing `onKeyDown` chain and early-return before `performTabCompletion` when the popup is open.

- [ ] **Step 1: Write the failing test**

```tsx
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { InputArea } from './input-area';
import { useCommandsStore } from '../stores/commands';
import { main } from '../../wailsjs/go/models';

vi.mock('../../wailsjs/go/main/App', () => ({ GetChannelInfo: vi.fn().mockResolvedValue({ users: [] }) }));

const mk = (name: string, usage = '', description = ''): main.CommandInfo =>
  ({ name, aliases: [], category: 'server', usage, description, source: '' } as main.CommandInfo);

describe('InputArea command autocomplete', () => {
  beforeEach(() => {
    useCommandsStore.setState({ commands: [mk('JOIN', '#channel [key]', 'Join a channel'), mk('QUIT', '[reason]', 'Disconnect')] });
  });

  it('shows a popup of matching commands when typing a slash command', () => {
    render(<InputArea onSendMessage={vi.fn()} networkId={1} channelName="#x" />);
    const input = screen.getByTestId('message-input');
    fireEvent.change(input, { target: { value: '/j' } });
    expect(screen.getByText('JOIN')).toBeInTheDocument();
    expect(screen.queryByText('QUIT')).not.toBeInTheDocument();
  });

  it('shows the usage hint after the command name', () => {
    render(<InputArea onSendMessage={vi.fn()} networkId={1} channelName="#x" />);
    const input = screen.getByTestId('message-input');
    fireEvent.change(input, { target: { value: '/join ' } });
    expect(screen.getByText(/#channel \[key\]/)).toBeInTheDocument();
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && npx vitest run src/components/input-area.command.test.tsx`
Expected: FAIL — no popup/hint rendered.

- [ ] **Step 3: Implement**

Add near the top of `InputArea` (after existing `useState` hooks):

```tsx
import { useCommandsStore } from '../stores/commands';
import { filterCommands, lookupCommand } from '../stores/commands';
import { parseCommandLine } from '../lib/command-line';
// ...
const commands = useCommandsStore((s) => s.commands);
const [cmdSelIndex, setCmdSelIndex] = useState(0);
const [cmdPopupDismissed, setCmdPopupDismissed] = useState(false);

const cmdLine = parseCommandLine(message);
const cmdMatches =
  cmdLine.isCommand && !cmdLine.afterCommandName ? filterCommands(commands, cmdLine.word) : [];
const showCmdPopup = cmdMatches.length > 0 && !cmdPopupDismissed;
const usageHint =
  cmdLine.isCommand && cmdLine.afterCommandName ? lookupCommand(commands, cmdLine.word) : null;

// Reset popup selection/dismissal as the typed command word changes.
useEffect(() => {
  setCmdSelIndex(0);
  setCmdPopupDismissed(false);
}, [cmdLine.word, cmdLine.afterCommandName]);

const acceptCommand = (info: { name: string }) => {
  setMessage(`/${info.name.toLowerCase()} `);
  setCmdPopupDismissed(true);
  setTimeout(() => inputRef.current?.focus(), 0);
};

const handleCommandKeys = (e: React.KeyboardEvent<HTMLInputElement>): boolean => {
  if (!showCmdPopup) return false;
  if (e.key === 'ArrowDown') {
    e.preventDefault();
    setCmdSelIndex((i) => (i + 1) % cmdMatches.length);
    return true;
  }
  if (e.key === 'ArrowUp') {
    e.preventDefault();
    setCmdSelIndex((i) => (i - 1 + cmdMatches.length) % cmdMatches.length);
    return true;
  }
  if (e.key === 'Enter' || e.key === 'Tab') {
    e.preventDefault();
    acceptCommand(cmdMatches[cmdSelIndex]);
    return true;
  }
  if (e.key === 'Escape') {
    e.preventDefault();
    setCmdPopupDismissed(true);
    return true;
  }
  return false;
};
```

Update the input's `onKeyDown` to consult the popup first:

```tsx
onKeyDown={(e) => {
  if (handleCommandKeys(e)) return;
  handleFormattingShortcut(e);
  handleHistoryNavigation(e);
  performTabCompletion(e);
}}
```

Render the popup + hint just above the `<form>` (inside the outer `div`, before `<MessagePreview>`):

```tsx
{showCmdPopup && (
  <div className="mb-2 max-h-48 overflow-y-auto rounded-md border border-border bg-background shadow-[var(--shadow-md)]" role="listbox">
    {cmdMatches.map((c, i) => (
      <div
        key={c.name}
        role="option"
        aria-selected={i === cmdSelIndex}
        className={`flex items-baseline gap-2 px-3 py-1.5 cursor-pointer text-sm ${i === cmdSelIndex ? 'bg-accent' : ''}`}
        onMouseDown={(e) => { e.preventDefault(); acceptCommand(c); }}
      >
        <span className="font-medium">/{c.name.toLowerCase()}</span>
        <span className="text-muted-foreground text-xs">{c.usage}</span>
        <span className="ml-auto text-muted-foreground/70 text-xs">{c.category}</span>
      </div>
    ))}
  </div>
)}
{usageHint && !showCmdPopup && (
  <div className="mb-2 px-1 text-xs text-muted-foreground">
    <span className="font-medium">/{usageHint.name.toLowerCase()}</span> {usageHint.usage}
    {usageHint.description ? <span className="text-muted-foreground/70"> — {usageHint.description}</span> : null}
  </div>
)}
```

- [ ] **Step 4: Run test**

Run: `cd frontend && npx vitest run src/components/input-area.command.test.tsx`
Expected: PASS

Run full frontend suite: `cd frontend && npx vitest run`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/input-area.tsx frontend/src/components/input-area.command.test.tsx
git commit -m "feat(commands): slash-command autocomplete popup + usage hint"
```

---

# Phase 3 — Help (buffer + panel + setting) and DM

## Task 8: help.display_mode preference

**Files:**
- Modify: `frontend/src/stores/preferences.ts`
- Test: `frontend/src/stores/preferences.help.test.ts`

**Interfaces:**
- Produces (added to `usePreferencesStore`): `helpDisplayMode: 'dialog' | 'buffer'`, `setHelpDisplayMode(mode)`. Key `help.display_mode`, default `'dialog'`. Hydrated in `initPreferences()`; reconciled via the existing `setting:changed` broadcast.

- [ ] **Step 1: Write the failing test**

```ts
import { describe, it, expect, vi, beforeEach } from 'vitest';

const setSetting = vi.fn().mockResolvedValue(undefined);
vi.mock('../../wailsjs/go/main/App', () => ({
  GetSetting: vi.fn().mockResolvedValue(''),
  SetSetting: (...a: unknown[]) => setSetting(...a),
}));
vi.mock('../../wailsjs/runtime/runtime', () => ({ EventsOn: vi.fn() }));

import { usePreferencesStore } from './preferences';

describe('helpDisplayMode preference', () => {
  beforeEach(() => { setSetting.mockClear(); usePreferencesStore.setState({ helpDisplayMode: 'dialog' }); });

  it('defaults to dialog', () => {
    expect(usePreferencesStore.getState().helpDisplayMode).toBe('dialog');
  });
  it('persists changes through SetSetting', () => {
    usePreferencesStore.getState().setHelpDisplayMode('buffer');
    expect(usePreferencesStore.getState().helpDisplayMode).toBe('buffer');
    expect(setSetting).toHaveBeenCalledWith('help.display_mode', 'buffer');
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && npx vitest run src/stores/preferences.help.test.ts`
Expected: FAIL — `helpDisplayMode` undefined.

- [ ] **Step 3: Implement** (edit `preferences.ts`)

Add constant + state. Mirror the `showFormattingToolbar` pattern:

```ts
const HELP_DISPLAY_MODE_KEY = 'help.display_mode';
type HelpDisplayMode = 'dialog' | 'buffer';
const DEFAULT_HELP_DISPLAY_MODE: HelpDisplayMode = 'dialog';
```

Extend `PreferencesState`:

```ts
  helpDisplayMode: HelpDisplayMode;
  setHelpDisplayMode: (mode: HelpDisplayMode) => void;
```

In the store body:

```ts
  helpDisplayMode: DEFAULT_HELP_DISPLAY_MODE,
  setHelpDisplayMode: (mode) => {
    set({ helpDisplayMode: mode });
    SetSetting(HELP_DISPLAY_MODE_KEY, mode).catch((error) => {
      console.error('Failed to persist helpDisplayMode:', error);
    });
  },
```

In `initPreferences()`, after the existing GetSetting block, add:

```ts
  try {
    const mode = await GetSetting(HELP_DISPLAY_MODE_KEY);
    if (mode === 'dialog' || mode === 'buffer') {
      usePreferencesStore.setState({ helpDisplayMode: mode });
    }
  } catch (error) {
    console.error('Failed to load help display mode:', error);
  }
```

And inside the existing `setting:changed` handler, add a branch:

```ts
    if (payload.key === HELP_DISPLAY_MODE_KEY && (payload.value === 'dialog' || payload.value === 'buffer')) {
      usePreferencesStore.setState({ helpDisplayMode: payload.value });
    }
```

- [ ] **Step 4: Run test**

Run: `cd frontend && npx vitest run src/stores/preferences.help.test.ts`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add frontend/src/stores/preferences.ts frontend/src/stores/preferences.help.test.ts
git commit -m "feat(help): help.display_mode preference (dialog/buffer)"
```

## Task 9: Help text formatting helper

**Files:**
- Create: `frontend/src/lib/help-format.ts`
- Test: `frontend/src/lib/help-format.test.ts`

**Interfaces:**
- Produces:
  - `formatCommandHelp(cmd: main.CommandInfo): string[]` — lines for `/help <cmd>` (usage + description + aliases).
  - `formatHelpList(commands: main.CommandInfo[]): string[]` — full categorized list (Client / Server / CTCP / Plugin headers, plugin commands grouped by source).

- [ ] **Step 1: Write the failing test**

```ts
import { describe, it, expect } from 'vitest';
import { formatCommandHelp, formatHelpList } from './help-format';
import { main } from '../../wailsjs/go/models';

const mk = (o: Partial<main.CommandInfo>): main.CommandInfo =>
  ({ name: 'X', aliases: [], category: 'server', usage: '', description: '', source: '', ...o } as main.CommandInfo);

describe('help-format', () => {
  it('formats a single command', () => {
    const lines = formatCommandHelp(mk({ name: 'JOIN', aliases: ['J'], usage: '#channel [key]', description: 'Join a channel' }));
    expect(lines.join('\n')).toContain('/join #channel [key]');
    expect(lines.join('\n')).toContain('Join a channel');
    expect(lines.join('\n')).toContain('J');
  });
  it('groups the full list by category', () => {
    const lines = formatHelpList([
      mk({ name: 'JOIN', category: 'server' }),
      mk({ name: 'QUERY', category: 'client' }),
      mk({ name: 'WEATHER', category: 'plugin', source: 'weather-plugin' }),
    ]);
    const text = lines.join('\n');
    expect(text).toMatch(/Client commands/i);
    expect(text).toMatch(/Server commands/i);
    expect(text).toMatch(/weather-plugin/i);
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && npx vitest run src/lib/help-format.test.ts`
Expected: FAIL — cannot import.

- [ ] **Step 3: Implement**

```ts
import { main } from '../../wailsjs/go/models';

export function formatCommandHelp(cmd: main.CommandInfo): string[] {
  const lines = [`/${cmd.name.toLowerCase()} ${cmd.usage}`.trim()];
  if (cmd.description) lines.push(`  ${cmd.description}`);
  if (cmd.aliases && cmd.aliases.length) {
    lines.push(`  Aliases: ${cmd.aliases.map((a) => '/' + a.toLowerCase()).join(', ')}`);
  }
  return lines;
}

export function formatHelpList(commands: main.CommandInfo[]): string[] {
  const lines: string[] = [];
  const section = (title: string, cmds: main.CommandInfo[]) => {
    if (!cmds.length) return;
    lines.push(`— ${title} —`);
    for (const c of [...cmds].sort((a, b) => a.name.localeCompare(b.name))) {
      lines.push(`/${c.name.toLowerCase()} ${c.usage}`.trim() + (c.description ? `  ${c.description}` : ''));
    }
  };
  section('Client commands', commands.filter((c) => c.category === 'client'));
  section('Server commands', commands.filter((c) => c.category === 'server'));
  section('CTCP commands', commands.filter((c) => c.category === 'ctcp'));

  const plugin = commands.filter((c) => c.category === 'plugin');
  const bySource = new Map<string, main.CommandInfo[]>();
  for (const c of plugin) {
    const arr = bySource.get(c.source) || [];
    arr.push(c);
    bySource.set(c.source, arr);
  }
  for (const [source, cmds] of bySource) section(`Plugin: ${source}`, cmds);
  return lines;
}
```

- [ ] **Step 4: Run test**

Run: `cd frontend && npx vitest run src/lib/help-format.test.ts`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add frontend/src/lib/help-format.ts frontend/src/lib/help-format.test.ts
git commit -m "feat(help): help text formatting helpers"
```

## Task 10: Backend PrintLocalLines sink for buffer help

`/help` is frontend-handled, but buffer output must render through the normal message pipeline (so it scrolls and styles like other system lines). Add a bound method that writes system messages to the active buffer and emits the standard message event.

**Files:**
- Create: `app_local_messages.go`
- Test: `app_local_messages_test.go`
- Regenerate: `frontend/wailsjs/`

**Interfaces:**
- Produces: `func (a *App) PrintLocalLines(networkID int64, target string, lines []string) error` — `target` is `""`/`"status"` for the status buffer, `"#channel"` for a channel, or a bare nick for a PM. Writes each line as a `storage.Message` with `MessageType: "system"` and the correct `ChannelID`/`PMTarget`, then emits `message-event` for the active buffer so the frontend reloads.

- [ ] **Step 1: Write the failing test**

```go
package main

import "testing"

func TestPrintLocalLinesValidation(t *testing.T) {
	a := &App{}
	if err := a.PrintLocalLines(1, "status", nil); err != nil {
		t.Fatalf("empty lines should be a no-op, got %v", err)
	}
}
```

(Storage-backed write paths are covered by existing storage tests; this asserts the no-op guard so the method is safe to call with nothing to print.)

- [ ] **Step 2: Run test to verify it fails**

Run: `CGO_ENABLED=1 go test -tags fts5 -run TestPrintLocalLines . -count=1`
Expected: FAIL — undefined: `PrintLocalLines`.

- [ ] **Step 3: Implement**

```go
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
		"type": string(irc.EventMessageReceived),
		"data": map[string]interface{}{"networkId": networkID, "channel": target},
		"timestamp": time.Now().Format(time.RFC3339),
	})
	return nil
}
```

(If `irc.EventMessageReceived` is not the right constant name for the message-event `type`, use the literal `"message.received"` to match what `App.OnEvent` forwards; confirm against `internal/irc/events.go`.)

- [ ] **Step 4: Run test + regen**

Run: `CGO_ENABLED=1 go test -tags fts5 -run TestPrintLocalLines . -count=1`
Expected: PASS

Run: `wails3 generate bindings -clean=true`
Expected: `PrintLocalLines` added to `frontend/wailsjs/go/main/App`.

- [ ] **Step 5: Commit**

```bash
git add app_local_messages.go app_local_messages_test.go frontend/wailsjs
git commit -m "feat(help): PrintLocalLines sink for buffer-mode help output"
```

## Task 11: Help dialog component + UI store flag + /help interception

**Files:**
- Modify: `frontend/src/stores/ui.ts` (add `helpOpen`, `setHelpOpen`)
- Create: `frontend/src/components/help-dialog.tsx`
- Modify: `frontend/src/App.tsx` (mount `<HelpDialog>`)
- Modify: `frontend/src/stores/network.ts` (`sendMessage`: intercept `/help`)
- Test: `frontend/src/stores/network.help.test.ts`

**Interfaces:**
- Consumes: `useCommandsStore`, `formatCommandHelp`, `formatHelpList`, `usePreferencesStore.helpDisplayMode`, `PrintLocalLines`, `useUIStore.setHelpOpen`, `lookupCommand`.
- Behavior in `sendMessage` (inside the existing `if (trimmedMessage.startsWith('/'))` block, BEFORE the `/me` handling): if the command word is `help`/`HELP`, handle locally and `return` without calling `SendCommand`:
  - With an argument: `lookupCommand`; if found, `PrintLocalLines(net, target, formatCommandHelp(cmd))`; else print `Unknown command: <arg>`.
  - No argument: if `helpDisplayMode === 'dialog'` → `setHelpOpen(true)`; else `PrintLocalLines(net, target, formatHelpList(commands))`.
  - `target` derives from `selectedChannel`: `'status'`→`'status'`, `pm:nick`→`nick`, else the channel name.

- [ ] **Step 1: Write the failing test**

```ts
import { describe, it, expect, vi, beforeEach } from 'vitest';

const printLocalLines = vi.fn().mockResolvedValue(undefined);
const sendCommand = vi.fn().mockResolvedValue(undefined);
vi.mock('../../wailsjs/go/main/App', () => ({
  PrintLocalLines: (...a: unknown[]) => printLocalLines(...a),
  SendCommand: (...a: unknown[]) => sendCommand(...a),
  // other App imports used by the store are unused in this test path:
  SendMessage: vi.fn(), SetPaneFocus: vi.fn(), RequestChatHistoryLatest: vi.fn(),
}));
vi.mock('../../wailsjs/runtime/runtime', () => ({ EventsOn: vi.fn(() => () => {}) }));

import { useNetworkStore } from './network';
import { useCommandsStore } from './commands';
import { usePreferencesStore } from './preferences';
import { useUIStore } from './ui';

describe('/help interception', () => {
  beforeEach(() => {
    printLocalLines.mockClear(); sendCommand.mockClear();
    useCommandsStore.setState({ commands: [{ name: 'JOIN', aliases: ['J'], category: 'server', usage: '#channel [key]', description: 'Join a channel', source: '' } as never] });
    useNetworkStore.setState({ selectedNetwork: 1, selectedChannel: 'status', networks: [{ id: 1, address: 'x' } as never] });
  });

  it('/help <cmd> prints to the buffer and does not hit SendCommand', async () => {
    await useNetworkStore.getState().sendMessage('/help join');
    expect(printLocalLines).toHaveBeenCalled();
    expect(sendCommand).not.toHaveBeenCalled();
  });

  it('/help with dialog mode opens the dialog', async () => {
    usePreferencesStore.setState({ helpDisplayMode: 'dialog' } as never);
    await useNetworkStore.getState().sendMessage('/help');
    expect(useUIStore.getState().helpOpen).toBe(true);
  });

  it('/help with buffer mode prints the list', async () => {
    usePreferencesStore.setState({ helpDisplayMode: 'buffer' } as never);
    await useNetworkStore.getState().sendMessage('/help');
    expect(printLocalLines).toHaveBeenCalled();
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && npx vitest run src/stores/network.help.test.ts`
Expected: FAIL — `/help` not intercepted.

- [ ] **Step 3: Implement**

In `frontend/src/stores/ui.ts`, add to the store state + actions:

```ts
  helpOpen: false,
  setHelpOpen: (open: boolean) => set({ helpOpen: open }),
```

(and add `helpOpen: boolean; setHelpOpen: (open: boolean) => void;` to its state interface.)

In `frontend/src/stores/network.ts`, add imports:

```ts
import { PrintLocalLines } from '../../wailsjs/go/main/App';
import { useCommandsStore, lookupCommand } from './commands';
import { usePreferencesStore } from './preferences';
import { useUIStore } from './ui';
import { formatCommandHelp, formatHelpList } from '../lib/help-format';
```

At the very start of the `if (trimmedMessage.startsWith('/'))` block in `sendMessage`:

```ts
      // /help is handled entirely client-side (it owns the cached registry).
      const helpMatch = trimmedMessage.match(/^\/help(?:\s+(\S+))?\s*$/i);
      if (helpMatch) {
        const target =
          selectedChannel === 'status'
            ? 'status'
            : selectedChannel.startsWith('pm:')
              ? selectedChannel.substring(3)
              : selectedChannel;
        const commands = useCommandsStore.getState().commands;
        const arg = helpMatch[1];
        if (arg) {
          const cmd = lookupCommand(commands, arg.replace(/^\//, ''));
          const lines = cmd ? formatCommandHelp(cmd) : [`Unknown command: ${arg}`];
          await PrintLocalLines(selectedNetwork, target, lines);
        } else if (usePreferencesStore.getState().helpDisplayMode === 'dialog') {
          useUIStore.getState().setHelpOpen(true);
        } else {
          await PrintLocalLines(selectedNetwork, target, formatHelpList(commands));
        }
        await loadMessages();
        return;
      }
```

Create `frontend/src/components/help-dialog.tsx`:

```tsx
import { useState } from 'react';
import { useUIStore } from '../stores/ui';
import { useCommandsStore } from '../stores/commands';
import { main } from '../../wailsjs/go/models';

const CATEGORY_TITLES: Record<string, string> = {
  client: 'Client commands',
  server: 'Server commands',
  ctcp: 'CTCP commands',
  plugin: 'Plugin commands',
};

export function HelpDialog() {
  const open = useUIStore((s) => s.helpOpen);
  const setOpen = useUIStore((s) => s.setHelpOpen);
  const commands = useCommandsStore((s) => s.commands);
  const [query, setQuery] = useState('');

  if (!open) return null;

  const q = query.toLowerCase();
  const filtered = commands.filter(
    (c) =>
      c.name.toLowerCase().includes(q) ||
      c.description.toLowerCase().includes(q) ||
      (c.aliases || []).some((a) => a.toLowerCase().includes(q))
  );
  const order = ['client', 'server', 'ctcp', 'plugin'];
  const grouped = order
    .map((cat) => ({ cat, items: filtered.filter((c) => c.category === cat) }))
    .filter((g) => g.items.length);

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40" onClick={() => setOpen(false)}>
      <div className="w-[36rem] max-h-[80vh] overflow-hidden rounded-lg border border-border bg-background shadow-[var(--shadow-lg)] flex flex-col" onClick={(e) => e.stopPropagation()}>
        <div className="p-4 border-b border-border">
          <input
            autoFocus
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search commands…"
            className="w-full px-3 py-2 rounded-md border border-border bg-background text-foreground focus:outline-none focus:ring-2 focus:ring-primary"
          />
        </div>
        <div className="overflow-y-auto p-2">
          {grouped.map((g) => (
            <div key={g.cat} className="mb-3">
              <div className="px-2 py-1 text-xs uppercase tracking-wide text-muted-foreground">{CATEGORY_TITLES[g.cat]}</div>
              {g.items.map((c: main.CommandInfo) => (
                <div key={`${c.source}:${c.name}`} className="px-2 py-1.5 rounded hover:bg-accent/60">
                  <div className="text-sm font-medium">/{c.name.toLowerCase()} <span className="text-muted-foreground font-normal">{c.usage}</span></div>
                  {c.description ? <div className="text-xs text-muted-foreground">{c.description}{c.source ? ` · ${c.source}` : ''}</div> : null}
                </div>
              ))}
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}
```

Mount it in `App.tsx` near other top-level overlays:

```tsx
import { HelpDialog } from './components/help-dialog';
// ... in the returned JSX, alongside other modals:
<HelpDialog />
```

- [ ] **Step 4: Run tests**

Run: `cd frontend && npx vitest run src/stores/network.help.test.ts`
Expected: PASS

Run full frontend suite: `cd frontend && npx vitest run`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add frontend/src/stores/ui.ts frontend/src/stores/network.ts frontend/src/components/help-dialog.tsx frontend/src/App.tsx frontend/src/stores/network.help.test.ts
git commit -m "feat(help): /help interception, buffer output, and help dialog"
```

## Task 12: DM on double-click + /query navigation + settings toggle

**Files:**
- Modify: `frontend/src/stores/network.ts` (`openQuery` action; `/query` navigation post-step)
- Modify: `frontend/src/components/channel-info.tsx` (double-click handler + `onOpenQuery` prop)
- Modify: parent that renders `ChannelInfo` (wire `onOpenQuery`) — locate via `grep -rn "<ChannelInfo" frontend/src`
- Modify: `frontend/src/components/settings-panel.tsx` (help display mode toggle)
- Test: `frontend/src/stores/network.query.test.ts`

**Interfaces:**
- Consumes: existing `SetPrivateMessageOpen` binding, `selectPane`.
- Produces: `useNetworkStore.openQuery(networkId: number, nick: string): Promise<void>` — calls `SetPrivateMessageOpen(networkId, nick, true)` then `selectPane(networkId, 'pm:'+nick)`. `ChannelInfo` gains optional `onOpenQuery?: (nick: string) => void` and an `onDoubleClick` on each user row.

- [ ] **Step 1: Write the failing test**

```ts
import { describe, it, expect, vi, beforeEach } from 'vitest';

const setPMOpen = vi.fn().mockResolvedValue(undefined);
const setPaneFocus = vi.fn().mockResolvedValue(undefined);
const requestHist = vi.fn().mockResolvedValue(undefined);
vi.mock('../../wailsjs/go/main/App', () => ({
  SetPrivateMessageOpen: (...a: unknown[]) => setPMOpen(...a),
  SetPaneFocus: (...a: unknown[]) => setPaneFocus(...a),
  RequestChatHistoryLatest: (...a: unknown[]) => requestHist(...a),
}));
vi.mock('../../wailsjs/runtime/runtime', () => ({ EventsOn: vi.fn(() => () => {}) }));

import { useNetworkStore } from './network';

describe('openQuery', () => {
  beforeEach(() => { setPMOpen.mockClear(); });
  it('opens the conversation and navigates to the pm pane', async () => {
    await useNetworkStore.getState().openQuery(1, 'alice');
    expect(setPMOpen).toHaveBeenCalledWith(1, 'alice', true);
    expect(useNetworkStore.getState().selectedChannel).toBe('pm:alice');
    expect(useNetworkStore.getState().selectedNetwork).toBe(1);
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && npx vitest run src/stores/network.query.test.ts`
Expected: FAIL — `openQuery` undefined.

- [ ] **Step 3: Implement**

In `network.ts`, add the action (and its type in the store interface) + import `SetPrivateMessageOpen`:

```ts
  openQuery: async (networkId, nick) => {
    try {
      await SetPrivateMessageOpen(networkId, nick, true);
    } catch (error) {
      console.error('Failed to open query:', error);
    }
    await get().selectPane(networkId, `pm:${nick}`);
  },
```

In the `/query` post-dispatch path (after `await SendCommand(...)` in `sendMessage`, only when the command is `/query` or `/q` without an inline message), navigate. Add just before `await loadMessages();` in the slash block:

```ts
      const queryMatch = trimmedMessage.match(/^\/(?:query|q)\s+(\S+)\s*$/i);
      if (queryMatch) {
        await get().selectPane(selectedNetwork, `pm:${queryMatch[1]}`);
      }
```

In `channel-info.tsx`, add `onOpenQuery?: (nick: string) => void;` to the props interface, and add a double-click handler to the user row (the `<div>` at ~line 540 that has `onContextMenu`):

```tsx
onDoubleClick={() => onOpenQuery?.(user.nickname)}
```

Wire it from the parent that renders `<ChannelInfo .../>`: pass `onOpenQuery={(nick) => useNetworkStore.getState().openQuery(networkId, nick)}` (use the network id already available in that component).

In `settings-panel.tsx`, add a toggle/segmented control bound to `usePreferencesStore`’s `helpDisplayMode` / `setHelpDisplayMode` (mirror the existing `showFormattingToolbar` control in that file):

```tsx
// Help display: dialog vs buffer
<label className="flex items-center justify-between py-2">
  <span>Show /help as</span>
  <select
    value={helpDisplayMode}
    onChange={(e) => setHelpDisplayMode(e.target.value as 'dialog' | 'buffer')}
    className="border border-border rounded px-2 py-1 bg-background"
  >
    <option value="dialog">Dialog</option>
    <option value="buffer">Buffer text</option>
  </select>
</label>
```

(Pull `helpDisplayMode` and `setHelpDisplayMode` from `usePreferencesStore` at the top of `settings-panel.tsx`.)

- [ ] **Step 4: Run tests**

Run: `cd frontend && npx vitest run src/stores/network.query.test.ts`
Expected: PASS

Run full frontend suite: `cd frontend && npx vitest run`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add frontend/src/stores/network.ts frontend/src/components/channel-info.tsx frontend/src/components/settings-panel.tsx frontend/src/stores/network.query.test.ts
git commit -m "feat(dm): double-click nick opens DM, /query navigates, help-mode toggle"
```

---

# Phase 4 — Plugin-registered slash commands

## Task 13: Protocol + PluginInfo command fields

**Files:**
- Modify: `internal/plugin/protocol.go`
- Test: `internal/plugin/protocol_test.go`

**Interfaces:**
- Produces:
  - `type CommandSpecWire struct { Name string `json:"name"`; Aliases []string `json:"aliases,omitempty"`; Usage string `json:"usage,omitempty"`; Description string `json:"description,omitempty"` }`
  - `InitializeResult.Commands []CommandSpecWire` (json `commands,omitempty`)
  - `PluginInfo.Commands []CommandSpecWire` (json `commands,omitempty`)

- [ ] **Step 1: Write the failing test**

```go
package plugin

import (
	"encoding/json"
	"testing"
)

func TestInitializeResultCommandsRoundTrip(t *testing.T) {
	in := InitializeResult{Name: "weather", Commands: []CommandSpecWire{{Name: "weather", Usage: "<city>", Description: "Show weather"}}}
	b, _ := json.Marshal(in)
	var out InitializeResult
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Commands) != 1 || out.Commands[0].Name != "weather" {
		t.Fatalf("commands did not round-trip: %+v", out.Commands)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `CGO_ENABLED=1 go test -tags fts5 ./internal/plugin/ -run TestInitializeResultCommands -count=1`
Expected: FAIL — `Commands` field / `CommandSpecWire` undefined.

- [ ] **Step 3: Implement** (edit `protocol.go`)

Add the type and fields:

```go
// CommandSpecWire is a slash command a plugin registers in its initialize response.
type CommandSpecWire struct {
	Name        string   `json:"name"`
	Aliases     []string `json:"aliases,omitempty"`
	Usage       string   `json:"usage,omitempty"`
	Description string   `json:"description,omitempty"`
}
```

Add `Commands []CommandSpecWire `json:"commands,omitempty"`` to both `InitializeResult` and `PluginInfo`.

- [ ] **Step 4: Run test**

Run: `CGO_ENABLED=1 go test -tags fts5 ./internal/plugin/ -run TestInitializeResultCommands -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/plugin/protocol.go internal/plugin/protocol_test.go
git commit -m "feat(plugin): declare slash commands in initialize protocol"
```

## Task 14: Plugin command table + conflict policy in Manager

**Files:**
- Modify: `internal/plugin/manager.go`
- Test: `internal/plugin/manager_commands_test.go`

**Interfaces:**
- Produces (on `Manager`):
  - field `pluginCommands map[string]pluginCommandEntry` (init in `NewManager`) where `type pluginCommandEntry struct { Plugin string; Spec CommandSpecWire }`, keyed by upper-cased name/alias.
  - `func (pm *Manager) registerPluginCommands(pluginName string, cmds []CommandSpecWire, isBuiltin func(string) bool)` — for each name+alias: skip+log if `isBuiltin(key)` or already taken by another plugin; otherwise register.
  - `func (pm *Manager) unregisterPluginCommands(pluginName string)` — drop all keys owned by `pluginName`.
  - `func (pm *Manager) PluginCommands() []pluginCommandEntry` — canonical (one per command) snapshot for `GetCommands`.
  - `func (pm *Manager) LookupPluginCommand(name string) (pluginCommandEntry, bool)`.

- [ ] **Step 1: Write the failing test**

```go
package plugin

import "testing"

func TestRegisterPluginCommandsConflicts(t *testing.T) {
	pm := &Manager{pluginCommands: map[string]pluginCommandEntry{}}
	builtins := map[string]bool{"JOIN": true, "J": true}
	isBuiltin := func(k string) bool { return builtins[k] }

	pm.registerPluginCommands("p1", []CommandSpecWire{{Name: "weather", Aliases: []string{"wx"}}}, isBuiltin)
	if _, ok := pm.LookupPluginCommand("weather"); !ok {
		t.Fatal("weather should be registered")
	}
	if _, ok := pm.LookupPluginCommand("WX"); !ok {
		t.Fatal("alias wx should be registered")
	}

	// Cannot shadow a built-in.
	pm.registerPluginCommands("p1", []CommandSpecWire{{Name: "join"}}, isBuiltin)
	if e, _ := pm.LookupPluginCommand("JOIN"); e.Plugin == "p1" {
		t.Fatal("plugin must not shadow built-in JOIN")
	}

	// Second plugin cannot steal an existing plugin command.
	pm.registerPluginCommands("p2", []CommandSpecWire{{Name: "weather"}}, isBuiltin)
	if e, _ := pm.LookupPluginCommand("weather"); e.Plugin != "p1" {
		t.Fatalf("first registration wins; got %q", e.Plugin)
	}

	pm.unregisterPluginCommands("p1")
	if _, ok := pm.LookupPluginCommand("weather"); ok {
		t.Fatal("weather should be gone after unregister")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `CGO_ENABLED=1 go test -tags fts5 ./internal/plugin/ -run TestRegisterPluginCommandsConflicts -count=1`
Expected: FAIL — undefined symbols.

- [ ] **Step 3: Implement** (edit `manager.go`)

Add the field to `Manager` struct: `pluginCommands map[string]pluginCommandEntry` and init in `NewManager`: `pluginCommands: make(map[string]pluginCommandEntry),`. Then:

```go
type pluginCommandEntry struct {
	Plugin string
	Spec   CommandSpecWire
}

func (pm *Manager) registerPluginCommands(pluginName string, cmds []CommandSpecWire, isBuiltin func(string) bool) {
	for _, c := range cmds {
		for _, key := range append([]string{c.Name}, c.Aliases...) {
			k := strings.ToUpper(key)
			if isBuiltin(k) {
				logger.Log.Warn().Str("plugin", pluginName).Str("command", k).Msg("Plugin command shadows a built-in; ignored")
				continue
			}
			if existing, taken := pm.pluginCommands[k]; taken {
				logger.Log.Warn().Str("plugin", pluginName).Str("command", k).Str("owner", existing.Plugin).Msg("Plugin command already registered; ignored")
				continue
			}
			pm.pluginCommands[k] = pluginCommandEntry{Plugin: pluginName, Spec: c}
		}
	}
}

func (pm *Manager) unregisterPluginCommands(pluginName string) {
	for k, e := range pm.pluginCommands {
		if e.Plugin == pluginName {
			delete(pm.pluginCommands, k)
		}
	}
}

func (pm *Manager) LookupPluginCommand(name string) (pluginCommandEntry, bool) {
	e, ok := pm.pluginCommands[strings.ToUpper(name)]
	return e, ok
}

// PluginCommands returns one entry per canonical command (keyed by Spec.Name).
func (pm *Manager) PluginCommands() []pluginCommandEntry {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	seen := map[string]bool{}
	out := []pluginCommandEntry{}
	for _, e := range pm.pluginCommands {
		if seen[strings.ToUpper(e.Spec.Name)] {
			continue
		}
		seen[strings.ToUpper(e.Spec.Name)] = true
		out = append(out, e)
	}
	return out
}
```

Ensure `manager.go` imports `"strings"`.

Wire registration into both load paths: in `LoadPlugin` and `loadPluginUnlocked`, right after `info.Commands = initResult.Commands` (add that assignment alongside the other `info.* = initResult.*` lines) and before `pm.plugins[info.Name] = plugin`, call:

```go
		pm.registerPluginCommands(info.Name, initResult.Commands, func(k string) bool {
			return pm.isBuiltinCommand(k)
		})
```

And in `UnloadPlugin` and the disable branch of `SetPluginEnabled`, after deleting from `pm.plugins`, call `pm.unregisterPluginCommands(name)`.

`isBuiltinCommand` is provided by the App layer (the plugin package must not import `main`). Add a field to `Manager`: `isBuiltinCommand func(string) bool` defaulting to `func(string) bool { return false }` in `NewManager`, set by the App in Task 16.

- [ ] **Step 4: Run test**

Run: `CGO_ENABLED=1 go test -tags fts5 ./internal/plugin/ -run TestRegisterPluginCommandsConflicts -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/plugin/manager.go internal/plugin/manager_commands_test.go
git commit -m "feat(plugin): register plugin commands with conflict policy"
```

## Task 15: command.invoke RPC + action producer wiring

**Files:**
- Modify: `internal/plugin/manager.go` (invoke method + `EnqueueAction`)
- Modify: `internal/plugin/ipc.go` (`handleNotification`: handle `"action"`)
- Test: `internal/plugin/ipc_action_test.go`

**Interfaces:**
- Produces:
  - `func (pm *Manager) EnqueueAction(a Action)` — non-blocking send onto `actionQueue` (drop + log if full).
  - `func (pm *Manager) InvokePluginCommand(pluginName, command string, args []string, networkID int64, channel string) error` — finds the plugin's IPC, `SendRequest("command.invoke", map[string]interface{}{...})`, returns an error on RPC error.
  - `ipc.handleNotification` handles `req.Method == "action"` → parse `ActionParams` → `pm.EnqueueAction(Action{PluginID: ipc.pluginID, Type: params.Type, Data: params.Data})`.

- [ ] **Step 1: Write the failing test**

```go
package plugin

import "testing"

func TestEnqueueActionDeliversToQueue(t *testing.T) {
	pm := &Manager{actionQueue: make(chan Action, 1)}
	pm.EnqueueAction(Action{PluginID: "p1", Type: "send_message", Data: map[string]interface{}{"target": "#x", "message": "hi"}})
	select {
	case got := <-pm.GetActionQueue():
		if got.Type != "send_message" || got.Data["target"] != "#x" {
			t.Fatalf("unexpected action: %+v", got)
		}
	default:
		t.Fatal("expected an action on the queue")
	}
}

func TestEnqueueActionDropsWhenFull(t *testing.T) {
	pm := &Manager{actionQueue: make(chan Action, 1)}
	pm.EnqueueAction(Action{PluginID: "p1", Type: "a"})
	// Second enqueue must not block/panic when the buffer is full.
	pm.EnqueueAction(Action{PluginID: "p1", Type: "b"})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `CGO_ENABLED=1 go test -tags fts5 ./internal/plugin/ -run TestEnqueueAction -count=1`
Expected: FAIL — `EnqueueAction` undefined.

- [ ] **Step 3: Implement**

In `manager.go`:

```go
// EnqueueAction queues a plugin-requested action for the App to process.
// Non-blocking: if the queue is full the action is dropped with a warning.
func (pm *Manager) EnqueueAction(a Action) {
	select {
	case pm.actionQueue <- a:
	default:
		logger.Log.Warn().Str("plugin", a.PluginID).Str("type", a.Type).Msg("Plugin action queue full; dropping action")
	}
}

// InvokePluginCommand sends a command.invoke request to the owning plugin.
func (pm *Manager) InvokePluginCommand(pluginName, command string, args []string, networkID int64, channel string) error {
	pm.mu.RLock()
	plugin, ok := pm.plugins[pluginName]
	pm.mu.RUnlock()
	if !ok {
		return fmt.Errorf("plugin not loaded: %s", pluginName)
	}
	resp, err := plugin.IPC.SendRequest("command.invoke", map[string]interface{}{
		"command":   command,
		"args":      args,
		"networkId": networkID,
		"channel":   channel,
	})
	if err != nil {
		return fmt.Errorf("plugin command failed: %w", err)
	}
	if resp.Error != nil {
		return fmt.Errorf("plugin command error: %s", resp.Error.Message)
	}
	return nil
}
```

In `ipc.go` `handleNotification`, after the `ui_metadata.set` block, add:

```go
	// Handle generic action notifications (e.g. plugins sending a message).
	if req.Method == "action" {
		params, ok := req.Params.(map[string]interface{})
		if !ok {
			if b, err := json.Marshal(req.Params); err == nil {
				var p map[string]interface{}
				if json.Unmarshal(b, &p) == nil {
					params, ok = p, true
				}
			}
		}
		if ok {
			atype, _ := params["type"].(string)
			data, _ := params["data"].(map[string]interface{})
			ipc.manager.EnqueueAction(Action{PluginID: ipc.pluginID, Type: atype, Data: data})
		}
		return
	}
```

- [ ] **Step 4: Run test**

Run: `CGO_ENABLED=1 go test -tags fts5 ./internal/plugin/ -run TestEnqueueAction -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/plugin/manager.go internal/plugin/ipc.go internal/plugin/ipc_action_test.go
git commit -m "feat(plugin): command.invoke RPC + action-queue producer wiring"
```

## Task 16: Merge plugin commands into dispatch + GetCommands

**Files:**
- Modify: `app.go` (set `pluginManager.isBuiltinCommand`)
- Modify: `app_commands.go` (`dispatchCommand`: plugin fallback)
- Modify: `commands_registry.go` (`GetCommands`: append plugin specs)
- Test: `app_commands_dispatch_test.go`

**Interfaces:**
- Consumes: `pm.LookupPluginCommand`, `pm.InvokePluginCommand`, `pm.PluginCommands`, `CommandRegistry.Lookup`.
- Produces: dispatch order built-in → plugin → raw; `GetCommands` includes `Category: "plugin"`, `Source: <pluginName>` entries.

- [ ] **Step 1: Write the failing test**

```go
func TestGetCommandsIncludesPlugin(t *testing.T) {
	// A registry-only App with a fake plugin manager is heavy; assert the merge
	// helper directly instead.
	builtins := buildBuiltinRegistry()
	plugins := []CommandInfo{{Name: "WEATHER", Category: "plugin", Usage: "<city>", Description: "Weather", Source: "weather"}}
	merged := mergeCommandInfos(builtins, plugins)
	var found bool
	for _, c := range merged {
		if c.Name == "WEATHER" && c.Source == "weather" && c.Category == "plugin" {
			found = true
		}
	}
	if !found {
		t.Fatal("merged command list missing the plugin command")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `CGO_ENABLED=1 go test -tags fts5 -run TestGetCommandsIncludesPlugin . -count=1`
Expected: FAIL — `mergeCommandInfos` undefined.

- [ ] **Step 3: Implement**

In `commands_registry.go`, add the helper and rewrite `GetCommands`:

```go
func mergeCommandInfos(r *CommandRegistry, plugin []CommandInfo) []CommandInfo {
	specs := r.Specs()
	out := make([]CommandInfo, 0, len(specs)+len(plugin))
	for _, s := range specs {
		out = append(out, specToInfo(s))
	}
	return append(out, plugin...)
}

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
```

(Remove the old `GetCommands` body from Task 4.) In `app_commands.go` `dispatchCommand`, before the raw-passthrough fallback, add the plugin branch:

```go
	if a.pluginManager != nil {
		if entry, ok := a.pluginManager.LookupPluginCommand(name); ok {
			channel := "" // best-effort context; the frontend sends the active buffer separately if needed
			return a.pluginManager.InvokePluginCommand(entry.Plugin, strings.ToUpper(name), args, networkID, channel)
		}
	}
```

In `app.go` `NewApp`, after `app.commands` and `pluginManager` are set, wire the built-in predicate:

```go
	pluginMgr.SetBuiltinCommandChecker(func(k string) bool {
		_, ok := app.commands.Lookup(k)
		return ok
	})
```

Add to `manager.go`: `func (pm *Manager) SetBuiltinCommandChecker(fn func(string) bool) { if fn != nil { pm.isBuiltinCommand = fn } }`.

- [ ] **Step 4: Run tests**

Run: `CGO_ENABLED=1 go test -tags fts5 -run 'TestGetCommands|TestSendCommand' . -count=1`
Expected: PASS

Run: `CGO_ENABLED=1 go test -tags fts5 ./... -count=1`
Expected: PASS

- [ ] **Step 5: Regenerate bindings + commit**

Run: `wails3 generate bindings -clean=true`

```bash
git add app.go app_commands.go commands_registry.go internal/plugin/manager.go frontend/wailsjs
git commit -m "feat(plugin): merge plugin commands into dispatch and GetCommands"
```

## Task 17: plugin.lifecycle event → frontend command refetch

**Files:**
- Modify: `internal/events/events.go` (or wherever event consts live — confirm via `grep -rn "EventMetadataUpdated" internal/events`)
- Modify: `internal/plugin/manager.go` (emit on load/unload/enable/disable)
- Modify: `app_events.go` (`OnEvent`: forward as `plugin-lifecycle`)
- Modify: `frontend/src/stores/commands.ts` (`initCommands`: subscribe to `plugin-lifecycle`)
- Test: `internal/plugin/manager_lifecycle_test.go`

**Interfaces:**
- Produces: `events.EventPluginLifecycle = "plugin.lifecycle"`. Manager emits `events.Event{Type: EventPluginLifecycle, Data: {"action": "loaded"|"unloaded", "plugin": name}}`. `OnEvent` forwards `a.emit("plugin-lifecycle", {...})`. Frontend refetches commands on receipt.

- [ ] **Step 1: Write the failing test**

```go
package plugin

import (
	"testing"

	"github.com/matt0x6f/irc-client/internal/events"
)

type capturingBus struct{ events []events.Event }

func (c *capturingBus) Emit(e events.Event) { c.events = append(c.events, e) }

func TestEmitPluginLifecycle(t *testing.T) {
	bus := events.NewEventBus()
	got := make(chan events.Event, 1)
	bus.Subscribe(events.EventPluginLifecycle, subscriberFunc(func(e events.Event) { got <- e }))
	pm := &Manager{eventBus: bus}
	pm.emitLifecycle("loaded", "weather")
	select {
	case e := <-got:
		if e.Data["plugin"] != "weather" || e.Data["action"] != "loaded" {
			t.Fatalf("unexpected lifecycle event: %+v", e.Data)
		}
	default:
		t.Fatal("no lifecycle event emitted")
	}
}

type subscriberFunc func(events.Event)

func (f subscriberFunc) OnEvent(e events.Event) { f(e) }
```

(If `events.EventBus.Emit` dispatches asynchronously, the test subscriber uses a buffered channel + `select` with a short retry; adjust to `EmitSync` in `emitLifecycle` if a synchronous guarantee is wanted — prefer `Emit` to match `HandleMetadataRequest`.)

- [ ] **Step 2: Run test to verify it fails**

Run: `CGO_ENABLED=1 go test -tags fts5 ./internal/plugin/ -run TestEmitPluginLifecycle -count=1`
Expected: FAIL — `EventPluginLifecycle` / `emitLifecycle` undefined.

- [ ] **Step 3: Implement**

Add the const next to `EventMetadataUpdated` in the events package:

```go
	EventPluginLifecycle = "plugin.lifecycle"
```

In `manager.go`:

```go
func (pm *Manager) emitLifecycle(action, plugin string) {
	if pm.eventBus == nil {
		return
	}
	pm.eventBus.Emit(events.Event{
		Type:      events.EventPluginLifecycle,
		Data:      map[string]interface{}{"action": action, "plugin": plugin},
		Timestamp: time.Now(),
		Source:    events.EventSourceSystem,
	})
}
```

Call `pm.emitLifecycle("loaded", info.Name)` at the end of `LoadPlugin` and `loadPluginUnlocked` (after `pm.plugins[info.Name] = plugin`), and `pm.emitLifecycle("unloaded", name)` at the end of `UnloadPlugin` and the disable branch of `SetPluginEnabled`. Ensure `manager.go` imports `time`.

In `app_events.go` `OnEvent`, alongside the `EventMetadataUpdated` block:

```go
	if event.Type == events.EventPluginLifecycle {
		a.emit("plugin-lifecycle", map[string]interface{}{
			"action": event.Data["action"],
			"plugin": event.Data["plugin"],
		})
		return
	}
```

In `frontend/src/stores/commands.ts`, make `initCommands` subscribe:

```ts
import { EventsOn } from '../../wailsjs/runtime/runtime';
// ...
export async function initCommands(): Promise<void> {
  await useCommandsStore.getState().loadCommands();
  EventsOn('plugin-lifecycle', () => {
    useCommandsStore.getState().loadCommands();
  });
}
```

- [ ] **Step 4: Run tests**

Run: `CGO_ENABLED=1 go test -tags fts5 ./internal/plugin/ -run TestEmitPluginLifecycle -count=1`
Expected: PASS

Run: `CGO_ENABLED=1 go test -tags fts5 ./... -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/events internal/plugin/manager.go app_events.go frontend/src/stores/commands.ts internal/plugin/manager_lifecycle_test.go
git commit -m "feat(plugin): plugin.lifecycle event refreshes the command list"
```

## Task 18: Docs

**Files:**
- Modify: `docs/plugin-system.md`

**Interfaces:** none (documentation).

- [ ] **Step 1: Document the additions**

Add a "Slash commands" section to `docs/plugin-system.md` covering:
- The `commands` array in the `initialize` response (`CommandSpecWire`: name, aliases, usage, description), with a JSON example.
- The host→plugin `command.invoke` request shape (`{command, args, networkId, channel}`) and that the plugin replies with a JSON-RPC result (error to report failure).
- The plugin→host `action` notification (`{type, data}`) and the supported `send_message` action (`{server, target, message}`), noting the queue is bounded and overflow drops actions.
- Conflict policy: plugins cannot shadow built-ins; first-registration-wins between plugins; collisions are logged and ignored.
- The `plugin-lifecycle` frontend event (informational).

- [ ] **Step 2: Verify the example**

Re-read the section; ensure the JSON `initialize` example is valid and field names match `CommandSpecWire` exactly (`name`, `aliases`, `usage`, `description`).

- [ ] **Step 3: Commit**

```bash
git add docs/plugin-system.md
git commit -m "docs(plugin): document plugin slash commands, command.invoke, actions"
```

---

## Phase → PR mapping

Each phase is an independently shippable PR (per the repo's plan-per-PR convention):
- PR 1: Tasks 1–4 (registry + dispatch + GetCommands)
- PR 2: Tasks 5–7 (autocomplete)
- PR 3: Tasks 8–12 (help + DM + setting)
- PR 4: Tasks 13–18 (plugin commands + docs)

## e2e screenshots (per quality bar — visual surfaces only)

After Phase 2 and Phase 3 land, add e2e screenshot coverage for the autocomplete popup and the help dialog (follow the existing e2e screenshot batch pattern; run Playwright from the `e2e/` dir using its local binary).
