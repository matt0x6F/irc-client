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
